/*
 *
 * Copyright 2023 casiusbot authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package main

import (
	"log"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
)

func main() {
	if godotenv.Overload() == nil {
		log.Println("Loaded .env file")
	}

	roleNameToPrefix, prefixes, err := readPrefixConfig("PREFIX_FILE_PATH")
	if err != nil {
		log.Fatalln("Cannot open the configuration file :", err)
	}

	okCmdMsg := os.Getenv("MESSAGE_CMD_OK")
	errPartialCmdMsg := os.Getenv("MESSAGE_CMD_PARTIAL_ERROR")
	errGlobalCmdMsg := os.Getenv("MESSAGE_CMD_GLOBAL_ERROR")
	errUnauthorizedCmdMsg := buildMsgWithPrefixList("MESSAGE_CMD_UNAUTHORIZED", roleNameToPrefix)

	guildId := os.Getenv("GUILD_ID")
	cmdRoles := strings.Split(os.Getenv("ROLES_CMD"), ",")
	defaultRole := os.Getenv("DEFAULT_ROLE")
	ignoredRoles := strings.Split(os.Getenv("IGNORED_ROLES"), ",")
	specialRoles := strings.Split(os.Getenv("SPECIAL_ROLES"), ",")
	gameList := getAndTrimSlice("GAME_LIST")
	updateGameInterval := 30 * time.Second
	targetNewsChannelName := os.Getenv("TARGET_NEWS_CHANNEL")
	feedURLs := getAndTrimSlice("FEED_URLS")
	checkInterval := getAndParseDurationSec("CHECK_INTERVAL")
	targetReminderChannelName := os.Getenv("TARGET_REMINDER_CHANNEL")
	reminderDelays := getAndParseDelayMins("REMINDER_BEFORES")
	reminderPrefix := buildReminderPrefix("REMINDER_TEXT", guildId)
	roleChannelName := os.Getenv("ROLE_CHANNEL")
	roleChannelCleaning := roleChannelName != ""

	applyCmd := &discordgo.ApplicationCommand{
		Name:        "apply-prefix",
		Description: "Apply the prefix rule to all User",
	}
	cleanCmd := &discordgo.ApplicationCommand{
		Name:        "clean-prefix",
		Description: "Clean the prefix for all User",
	}

	session, err := discordgo.New("Bot " + os.Getenv("BOT_TOKEN"))
	if err != nil {
		log.Fatalln("Cannot create the bot :", err)
	}
	session.Identify.Intents |= discordgo.IntentGuildMembers

	err = session.Open()
	if err != nil {
		log.Fatalln("Cannot open the session :", err)
	}
	defer session.Close()

	guild, err := session.Guild(guildId)
	if err != nil {
		log.Println("Cannot retrieve the guild :", err)
		return
	}
	ownerId := guild.OwnerID
	guildRoles := guild.Roles
	// emptying data no longer useful for GC cleaning
	guild = nil

	guildChannels, err := session.GuildChannels(guildId)
	if err != nil {
		log.Println("Cannot retrieve the guild channels :", err)
		return
	}

	roleChannelId := ""
	targetNewsChannelId := ""
	targetReminderChannelId := ""
	for _, channel := range guildChannels {
		channelName := channel.Name
		if channelName == targetNewsChannelName {
			targetNewsChannelId = channel.ID
		}
		// not in else statement (could be the same channel)
		if channelName == targetReminderChannelName {
			targetReminderChannelId = channel.ID
		}
		if roleChannelCleaning && channelName == roleChannelName {
			roleChannelId = channel.ID
		}
	}
	if targetNewsChannelId == "" {
		log.Println("Cannot retrieve the guild channel :", targetNewsChannelName)
		return
	}
	if targetReminderChannelId == "" {
		log.Println("Cannot retrieve the guild channel (2) :", targetReminderChannelName)
		return
	}
	if roleChannelCleaning && roleChannelId == "" {
		log.Println("Cannot retrieve the guild channel (3) :", roleChannelName)
		return
	}
	// emptying data no longer useful for GC cleaning
	guildChannels = nil
	roleChannelName = ""
	targetNewsChannelName = ""
	targetReminderChannelName = ""

	roleIdToPrefix := map[string]string{}
	roleNameToId := map[string]string{}
	for _, guildRole := range guildRoles {
		name := guildRole.Name
		id := guildRole.ID
		roleNameToId[name] = id
		if prefix, ok := roleNameToPrefix[name]; ok {
			roleIdToPrefix[id] = prefix
		}
	}
	// emptying data no longer useful for GC cleaning
	roleNameToPrefix = nil
	guildRoles = nil

	cmdRoleIds := initSetId(cmdRoles, roleNameToId)
	// emptying data no longer useful for GC cleaning
	cmdRoles = nil

	defaultRoleId := roleNameToId[defaultRole]
	// emptying data no longer useful for GC cleaning
	defaultRole = ""

	ignoredRoleIds := initSetId(ignoredRoles, roleNameToId)
	// emptying data no longer useful for GC cleaning
	ignoredRoles = nil

	specialRoleIds := initSetId(specialRoles, roleNameToId)
	// emptying data no longer useful for GC cleaning
	specialRoles = nil
	roleNameToId = nil

	guildMembers, err := session.GuildMembers(guildId, "", 1000)
	if err != nil {
		log.Println("Cannot retrieve members of the guild :", err)
		return
	}

	if roleChannelCleaning {
		initRoleChannelCleaning(session, guildMembers, roleChannelId, len(roleIdToPrefix))
	}

	var cmdworking boolAtom
	counterError := applyPrefixes(session, guildMembers, guildId, ownerId, defaultRoleId, ignoredRoleIds, specialRoleIds, roleIdToPrefix, prefixes, &cmdworking)
	if counterError != 0 {
		log.Println("Trying to apply-prefix at startup generate errors :", counterError)
	}
	// emptying data no longer useful for GC cleaning
	guildMembers = nil

	session.AddHandler(func(s *discordgo.Session, u *discordgo.GuildMemberUpdate) {
		if userId := u.User.ID; userId != ownerId {
			if !cmdworking.Get() {
				applyPrefix(s, u.Member, u.GuildID, ownerId, defaultRoleId, ignoredRoleIds, specialRoleIds, roleIdToPrefix, prefixes)
			}
		}
	})

	session.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		switch i.ApplicationCommandData().Name {
		case applyCmd.Name:
			membersCmd(s, i.GuildID, roleIdInSet(i.Member.Roles, cmdRoleIds), okCmdMsg, errPartialCmdMsg, errGlobalCmdMsg, errUnauthorizedCmdMsg, i.Interaction, func(guildMembers []*discordgo.Member) int {
				return applyPrefixes(s, guildMembers, i.GuildID, ownerId, defaultRoleId, ignoredRoleIds, specialRoleIds, roleIdToPrefix, prefixes, &cmdworking)
			})
		case cleanCmd.Name:
			membersCmd(s, i.GuildID, roleIdInSet(i.Member.Roles, cmdRoleIds), okCmdMsg, errPartialCmdMsg, errGlobalCmdMsg, errUnauthorizedCmdMsg, i.Interaction, func(guildMembers []*discordgo.Member) int {
				return cleanPrefixes(s, guildMembers, i.GuildID, ownerId, prefixes, &cmdworking)
			})
		}
	})

	appId := session.State.User.ID
	applyCmd, err = session.ApplicationCommandCreate(appId, guildId, applyCmd)
	if err != nil {
		log.Println("Cannot create apply command :", err)
	}

	cleanCmd, err = session.ApplicationCommandCreate(appId, guildId, cleanCmd)
	if err != nil {
		log.Println("Cannot create clean command :", err)
	}

	messageSender := createMessageSender(session, targetNewsChannelId)

	go updateGameStatus(session, gameList, updateGameInterval)

	feedNumber := len(feedURLs)
	tickers := launchTickers(feedNumber+1, checkInterval)

	startTime := time.Now().Add(-checkInterval).Add(-getAndParseDurationSec("INITIAL_BACKWARD_LOADING"))
	bgReadMultipleRSS(messageSender, feedURLs, startTime, tickers)
	go remindEvent(session, guildId, reminderDelays, targetReminderChannelId, reminderPrefix, startTime, tickers[feedNumber])

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	log.Println("Press Ctrl+C to exit")
	<-stop

	if err = session.ApplicationCommandDelete(appId, guildId, applyCmd.ID); err != nil {
		log.Println("Cannot delete apply command :", err)
	}
	if err = session.ApplicationCommandDelete(appId, guildId, cleanCmd.ID); err != nil {
		log.Println("Cannot delete clean command :", err)
	}
}
