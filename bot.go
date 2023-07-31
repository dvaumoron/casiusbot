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

	roleNameToPrefix, prefixes, cmdToRoleName, specialRoles, err := readPrefixConfig("PREFIX_FILE_PATH")
	if err != nil {
		log.Fatalln("Cannot read the configuration file :", err)
	}

	okCmdMsg := strings.TrimSpace(os.Getenv("MESSAGE_CMD_OK"))
	errPartialCmdMsg := strings.TrimSpace(os.Getenv("MESSAGE_CMD_PARTIAL_ERROR")) + " "
	errGlobalCmdMsg := strings.TrimSpace(os.Getenv("MESSAGE_CMD_GLOBAL_ERROR"))
	errUnauthorizedCmdMsg := buildMsgWithNameValueList(strings.TrimSpace(os.Getenv("MESSAGE_CMD_UNAUTHORIZED")), roleNameToPrefix)
	countCmdMsg := strings.TrimSpace(os.Getenv("MESSAGE_CMD_COUNT"))
	prefixMsg := strings.TrimSpace(os.Getenv("MESSAGE_PREFIX"))
	noChangeMsg := strings.TrimSpace(os.Getenv("MESSAGE_NO_CHANGE"))
	msgs := [...]string{okCmdMsg, errUnauthorizedCmdMsg, errGlobalCmdMsg, errPartialCmdMsg, countCmdMsg, prefixMsg, noChangeMsg}

	guildId := strings.TrimSpace(os.Getenv("GUILD_ID"))
	authorizedRoles := strings.Split(os.Getenv("AUTHORIZED_ROLES"), ",")
	forbiddenRoles := strings.Split(os.Getenv("FORBIDDEN_ROLES"), ",")
	joiningRole := strings.TrimSpace(os.Getenv("JOINING_ROLE"))
	defaultRole := strings.TrimSpace(os.Getenv("DEFAULT_ROLE"))
	ignoredRoles := strings.Split(os.Getenv("IGNORED_ROLES"), ",")
	gameList := getAndTrimSlice("GAME_LIST")
	updateGameInterval := 30 * time.Second
	targetPrefixChannelName := strings.TrimSpace(os.Getenv("TARGET_PREFIX_CHANNEL"))
	targetNewsChannelName := strings.TrimSpace(os.Getenv("TARGET_NEWS_CHANNEL"))
	feedURLs := getAndTrimSlice("FEED_URLS")
	checkInterval := getAndParseDurationSec("CHECK_INTERVAL")
	targetReminderChannelName := strings.TrimSpace(os.Getenv("TARGET_REMINDER_CHANNEL"))
	reminderDelays := getAndParseDelayMins("REMINDER_BEFORES")
	reminderPrefix := buildReminderPrefix("REMINDER_TEXT", guildId)

	applyCmd := newCommand("APPLY_CMD", "DESCRIPTION_APPLY_CMD")
	cleanCmd := newCommand("CLEAN_CMD", "DESCRIPTION_CLEAN_CMD")
	resetCmd := newCommand("RESET_CMD", "DESCRIPTION_RESET_CMD")
	countCmd := newCommand("COUNT_CMD", "DESCRIPTION_COUNT_CMD")

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
		return // to allow defer
	}
	ownerId := guild.OwnerID
	guildRoles := guild.Roles
	// emptying data no longer useful for GC cleaning
	guild = nil

	guildChannels, err := session.GuildChannels(guildId)
	if err != nil {
		log.Println("Cannot retrieve the guild channels :", err)
		return // to allow defer
	}

	targetPrefixChannelId := ""
	targetNewsChannelId := ""
	targetReminderChannelId := ""
	for _, channel := range guildChannels {
		// multiple if with no else statement (could be the same channel)
		channelName := channel.Name
		if channelName == targetPrefixChannelName {
			targetPrefixChannelId = channel.ID
		}
		if channelName == targetNewsChannelName {
			targetNewsChannelId = channel.ID
		}
		if channelName == targetReminderChannelName {
			targetReminderChannelId = channel.ID
		}
	}
	if targetNewsChannelId == "" {
		log.Println("Cannot retrieve the guild channel :", targetNewsChannelName)
		return // to allow defer
	}
	if targetReminderChannelId == "" {
		log.Println("Cannot retrieve the guild channel (2) :", targetReminderChannelName)
		return // to allow defer
	}
	if targetPrefixChannelName == "" {
		log.Println("Cannot retrieve the guild channel (3) :", targetPrefixChannelName)
		return // to allow defer
	}
	// emptying data no longer useful for GC cleaning
	guildChannels = nil
	targetPrefixChannelName = ""
	targetNewsChannelName = ""
	targetReminderChannelName = ""

	roleIdToPrefix := map[string]string{}
	roleNameToId := map[string]string{}
	roleIdToDisplayName := map[string]string{}
	for _, guildRole := range guildRoles {
		name := guildRole.Name
		id := guildRole.ID
		roleNameToId[name] = id
		displayName := name
		if prefix, ok := roleNameToPrefix[name]; ok {
			roleIdToPrefix[id] = prefix
			var buffer strings.Builder
			buffer.WriteString(name)
			buffer.WriteByte(' ')
			buffer.WriteString(prefix)
			displayName = buffer.String()
		}
		roleIdToDisplayName[id] = displayName
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

	authorizedRoleIds := initIdSet(authorizedRoles, roleNameToId)
	// emptying data no longer useful for GC cleaning
	authorizedRoles = nil

	forbiddenRoleIds := initIdSet(forbiddenRoles, roleNameToId)
	// emptying data no longer useful for GC cleaning
	forbiddenRoles = nil

	joiningRoleId := roleNameToId[joiningRole]
	// emptying data no longer useful for GC cleaning
	joiningRole = ""

	defaultRoleId := roleNameToId[defaultRole]
	// emptying data no longer useful for GC cleaning
	defaultRole = ""

	ignoredRoleIds := initIdSet(ignoredRoles, roleNameToId)
	// emptying data no longer useful for GC cleaning
	ignoredRoles = nil

	specialRoleIds := initIdSet(specialRoles, roleNameToId)
	// emptying data no longer useful for GC cleaning
	specialRoles = nil
	roleNameToId = nil

	guildMembers, err := session.GuildMembers(guildId, "", 1000)
	if err != nil {
		log.Println("Cannot retrieve members of the guild :", err)
		return // to allow defer
	}

	channelManager := ChannelSenderManager{}
	channelManager.AddChannel(session, targetPrefixChannelId)
	channelManager.AddChannel(session, targetNewsChannelId)
	channelManager.AddChannel(session, targetReminderChannelId)
	prefixChannelSender := channelManager[targetPrefixChannelId]

	var cmdworking boolAtom
	counterError := applyPrefixes(session, guildMembers, guildId, ownerId, defaultRoleId, ignoredRoleIds, specialRoleIds, forbiddenRoleIds, roleIdToPrefix, prefixes, msgs, &cmdworking)
	if counterError != 0 {
		log.Println("Trying to apply-prefix at startup generate errors :", counterError)
	}
	// emptying data no longer useful for GC cleaning
	guildMembers = nil

	session.AddHandler(func(s *discordgo.Session, r *discordgo.GuildMemberAdd) {
		if err := s.GuildMemberRoleAdd(guildId, r.User.ID, joiningRoleId); err != nil {
			log.Println("Joining role addition failed :", err)
		}
	})

	session.AddHandler(func(s *discordgo.Session, u *discordgo.GuildMemberUpdate) {
		if userId := u.User.ID; userId != ownerId && !cmdworking.Get() {
			applyPrefix(s, prefixChannelSender, u.Member, guildId, ownerId, defaultRoleId, ignoredRoleIds, specialRoleIds, forbiddenRoleIds, roleIdToPrefix, prefixes, msgs)
		}
	})

	defaultRoleDisplayName := roleIdToDisplayName[defaultRoleId]
	execCmds := map[string]func(*discordgo.Session, *discordgo.InteractionCreate){
		applyCmd.Name: func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			guildId := i.GuildID
			membersCmd(s, guildId, idInSet(i.Member.Roles, authorizedRoleIds), msgs, i.Interaction, func(guildMembers []*discordgo.Member) int {
				return applyPrefixes(s, guildMembers, guildId, ownerId, defaultRoleId, ignoredRoleIds, specialRoleIds, forbiddenRoleIds, roleIdToPrefix, prefixes, msgs, &cmdworking)
			})
		},
		cleanCmd.Name: func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			guildId := i.GuildID
			membersCmd(s, guildId, idInSet(i.Member.Roles, authorizedRoleIds), msgs, i.Interaction, func(guildMembers []*discordgo.Member) int {
				return cleanPrefixes(s, guildMembers, guildId, ownerId, prefixes, &cmdworking)
			})
		},
		resetCmd.Name: func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			addRoleCmd(s, i, prefixChannelSender, ownerId, defaultRoleId, defaultRoleDisplayName, specialRoleIds, forbiddenRoleIds, roleIdToPrefix, &cmdworking, msgs)
		},
		countCmd.Name: func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			countRoleCmd(s, i, prefixChannelSender, roleIdToDisplayName, msgs)
		},
	}

	for _, cmd := range roleCmds {
		addedRoleId := cmdToRoleId[cmd.Name]
		addedRoleDisplayName := roleIdToDisplayName[addedRoleId]
		execCmds[cmd.Name] = func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			addRoleCmd(s, i, prefixChannelSender, ownerId, addedRoleId, addedRoleDisplayName, specialRoleIds, forbiddenRoleIds, roleIdToPrefix, &cmdworking, msgs)
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

	go updateGameStatus(session, gameList, updateGameInterval)

	feedNumber := len(feedURLs)
	tickers := launchTickers(feedNumber+1, checkInterval)

	startTime := time.Now().Add(-checkInterval).Add(-getAndParseDurationSec("INITIAL_BACKWARD_LOADING"))
	bgReadMultipleRSS(channelManager[targetNewsChannelId], feedURLs, startTime, tickers)
	go remindEvent(session, guildId, reminderDelays, channelManager[targetReminderChannelId], reminderPrefix, startTime, tickers[feedNumber])

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
