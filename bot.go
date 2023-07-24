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

	roleNameToPrefix, prefixes, cmdToRoleName, err := readPrefixConfig("PREFIX_FILE_PATH")
	if err != nil {
		log.Fatalln("Cannot read the configuration file :", err)
	}

	okCmdMsg := os.Getenv("MESSAGE_CMD_OK")
	errPartialCmdMsg := os.Getenv("MESSAGE_CMD_PARTIAL_ERROR")
	errGlobalCmdMsg := os.Getenv("MESSAGE_CMD_GLOBAL_ERROR")
	errUnauthorizedCmdMsg := buildMsgWithNameValueList(os.Getenv("MESSAGE_CMD_UNAUTHORIZED"), roleNameToPrefix)
	countCmdMsg := os.Getenv("MESSAGE_CMD_COUNT")

	guildId := os.Getenv("GUILD_ID")
	authorizedRoles := strings.Split(os.Getenv("AUTHORIZED_ROLES"), ",")
	forbiddenRoles := strings.Split(os.Getenv("FORBIDDEN_ROLES"), ",")
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

	applyCmd := &discordgo.ApplicationCommand{
		Name:        strings.TrimSpace(os.Getenv("APPLY_CMD")),
		Description: strings.TrimSpace(os.Getenv("DESCRIPTION_APPLY_CMD")),
	}
	cleanCmd := &discordgo.ApplicationCommand{
		Name:        strings.TrimSpace(os.Getenv("CLEAN_CMD")),
		Description: strings.TrimSpace(os.Getenv("DESCRIPTION_CLEAN_CMD")),
	}
	resetCmd := &discordgo.ApplicationCommand{
		Name:        strings.TrimSpace(os.Getenv("RESET_CMD")),
		Description: strings.TrimSpace(os.Getenv("DESCRIPTION_RESET_CMD")),
	}
	countCmd := &discordgo.ApplicationCommand{
		Name:        strings.TrimSpace(os.Getenv("COUNT_CMD")),
		Description: strings.TrimSpace(os.Getenv("DESCRIPTION_COUNT_CMD")),
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
	}
	if targetNewsChannelId == "" {
		log.Println("Cannot retrieve the guild channel :", targetNewsChannelName)
		return
	}
	if targetReminderChannelId == "" {
		log.Println("Cannot retrieve the guild channel (2) :", targetReminderChannelName)
		return
	}
	// emptying data no longer useful for GC cleaning
	guildChannels = nil
	targetNewsChannelName = ""
	targetReminderChannelName = ""

	roleIdToPrefix := map[string]string{}
	roleNameToId := map[string]string{}
	roleIdToName := map[string]string{}
	for _, guildRole := range guildRoles {
		name := guildRole.Name
		id := guildRole.ID
		roleNameToId[name] = id
		if prefix, ok := roleNameToPrefix[name]; ok {
			roleIdToPrefix[id] = prefix
		}
		roleIdToName[id] = name
	}
	// emptying data no longer useful for GC cleaning
	roleNameToPrefix = nil
	guildRoles = nil

	roleCmdDesc := strings.TrimSpace(os.Getenv("DESCRIPTION_ROLE_CMD")) + " "
	roleCmds := make([]*discordgo.ApplicationCommand, 0, len(cmdToRoleName))
	cmdToRoleId := map[string]string{}
	for cmd, roleName := range cmdToRoleName {
		roleCmds = append(roleCmds, &discordgo.ApplicationCommand{
			Name:        cmd,
			Description: roleCmdDesc + roleName,
		})
		cmdToRoleId[cmd] = roleNameToId[roleName]
	}
	// emptying data no longer useful for GC cleaning
	cmdToRoleName = nil

	authorizedRoleIds := initSetId(authorizedRoles, roleNameToId)
	// emptying data no longer useful for GC cleaning
	authorizedRoles = nil

	forbiddenRoleIds := initSetId(forbiddenRoles, roleNameToId)
	// emptying data no longer useful for GC cleaning
	forbiddenRoles = nil

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

	var cmdworking boolAtom
	counterError := applyPrefixes(session, guildMembers, guildId, ownerId, defaultRoleId, ignoredRoleIds, specialRoleIds, forbiddenRoleIds, roleIdToPrefix, prefixes, &cmdworking)
	if counterError != 0 {
		log.Println("Trying to apply-prefix at startup generate errors :", counterError)
	}
	// emptying data no longer useful for GC cleaning
	guildMembers = nil

	session.AddHandler(func(s *discordgo.Session, r *discordgo.GuildMemberAdd) {
		s.GuildMemberRoleAdd(guildId, r.User.ID, defaultRoleId)
	})

	session.AddHandler(func(s *discordgo.Session, u *discordgo.GuildMemberUpdate) {
		if userId := u.User.ID; userId != ownerId {
			if !cmdworking.Get() {
				applyPrefix(s, u.Member, u.GuildID, ownerId, defaultRoleId, ignoredRoleIds, specialRoleIds, forbiddenRoleIds, roleIdToPrefix, prefixes)
			}
		}
	})

	execCmds := map[string]func(*discordgo.Session, *discordgo.InteractionCreate){
		applyCmd.Name: func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			membersCmd(s, i.GuildID, roleIdInSet(i.Member.Roles, authorizedRoleIds), okCmdMsg, errPartialCmdMsg, errGlobalCmdMsg, errUnauthorizedCmdMsg, i.Interaction, func(guildMembers []*discordgo.Member) int {
				return applyPrefixes(s, guildMembers, i.GuildID, ownerId, defaultRoleId, ignoredRoleIds, specialRoleIds, forbiddenRoleIds, roleIdToPrefix, prefixes, &cmdworking)
			})
		},
		cleanCmd.Name: func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			membersCmd(s, i.GuildID, roleIdInSet(i.Member.Roles, authorizedRoleIds), okCmdMsg, errPartialCmdMsg, errGlobalCmdMsg, errUnauthorizedCmdMsg, i.Interaction, func(guildMembers []*discordgo.Member) int {
				return cleanPrefixes(s, guildMembers, i.GuildID, ownerId, prefixes, &cmdworking)
			})
		},
		resetCmd.Name: func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			addRoleCmd(s, i, ownerId, defaultRoleId, specialRoleIds, forbiddenRoleIds, roleIdToPrefix, prefixes, &cmdworking, okCmdMsg, errGlobalCmdMsg, errUnauthorizedCmdMsg)
		},
		countCmd.Name: func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			countRoleCmd(s, i, roleIdToName, countCmdMsg, errGlobalCmdMsg)
		},
	}

	for _, cmd := range roleCmds {
		addedRoleId := cmdToRoleId[cmd.Name]
		execCmds[cmd.Name] = func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			addRoleCmd(s, i, ownerId, addedRoleId, specialRoleIds, forbiddenRoleIds, roleIdToPrefix, prefixes, &cmdworking, okCmdMsg, errGlobalCmdMsg, errUnauthorizedCmdMsg)
		}
	}
	// emptying data no longer useful for GC cleaning
	cmdToRoleId = nil

	session.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if execCmd, ok := execCmds[i.ApplicationCommandData().Name]; ok {
			execCmd(s, i)
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
	for index, cmd := range roleCmds {
		if roleCmds[index], err = session.ApplicationCommandCreate(appId, guildId, cmd); err != nil {
			log.Println("Cannot create", cmd.Name, "command :", err)
		}
	}
	resetCmd, err = session.ApplicationCommandCreate(appId, guildId, resetCmd)
	if err != nil {
		log.Println("Cannot create reset command :", err)
	}
	countCmd, err = session.ApplicationCommandCreate(appId, guildId, countCmd)
	if err != nil {
		log.Println("Cannot create count command :", err)
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
	for _, cmd := range roleCmds {
		if err = session.ApplicationCommandDelete(appId, guildId, cmd.ID); err != nil {
			log.Println("Cannot delete", cmd.Name, "command :", err)
		}
	}
	if err = session.ApplicationCommandDelete(appId, guildId, resetCmd.ID); err != nil {
		log.Println("Cannot delete reset command :", err)
	}
	if err = session.ApplicationCommandDelete(appId, guildId, countCmd.ID); err != nil {
		log.Println("Cannot delete count command :", err)
	}
}
