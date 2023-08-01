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

	roleNameToPrefix, prefixes, cmdToRoleName, specialRoles := readPrefixConfig("PREFIX_FILE_PATH")

	okCmdMsg := strings.TrimSpace(os.Getenv("MESSAGE_CMD_OK"))
	runningCmdMsg := strings.TrimSpace(os.Getenv("MESSAGE_CMD_RUNNING"))
	endedCmdMsg := strings.TrimSpace(os.Getenv("MESSAGE_CMD_ENDED"))
	errPartialCmdMsg := strings.TrimSpace(os.Getenv("MESSAGE_CMD_PARTIAL_ERROR")) + " "
	errGlobalCmdMsg := strings.TrimSpace(os.Getenv("MESSAGE_CMD_GLOBAL_ERROR"))
	errUnauthorizedCmdMsg := buildMsgWithNameValueList(strings.TrimSpace(os.Getenv("MESSAGE_CMD_UNAUTHORIZED")), roleNameToPrefix)
	countCmdMsg := strings.TrimSpace(os.Getenv("MESSAGE_CMD_COUNT"))
	prefixMsg := strings.TrimSpace(os.Getenv("MESSAGE_PREFIX"))
	noChangeMsg := strings.TrimSpace(os.Getenv("MESSAGE_NO_CHANGE"))
	msgs := [...]string{okCmdMsg, errUnauthorizedCmdMsg, errGlobalCmdMsg, errPartialCmdMsg, countCmdMsg, prefixMsg, noChangeMsg, endedCmdMsg, runningCmdMsg}

	guildId := requireConf("GUILD_ID")
	joiningRole := strings.TrimSpace(os.Getenv("JOINING_ROLE"))
	defaultRole := strings.TrimSpace(os.Getenv("DEFAULT_ROLE"))
	gameList := getAndTrimSlice("GAME_LIST")
	updateGameInterval := 30 * time.Second
	targetPrefixChannelName := strings.TrimSpace(os.Getenv("TARGET_PREFIX_CHANNEL"))
	targetCmdChannelName := strings.TrimSpace(os.Getenv("TARGET_CMD_CHANNEL"))
	targetNewsChannelName := strings.TrimSpace(os.Getenv("TARGET_NEWS_CHANNEL"))
	feedURLs := getAndTrimSlice("FEED_URLS")
	checkInterval := getAndParseDurationSec("CHECK_INTERVAL")
	targetReminderChannelName := strings.TrimSpace(os.Getenv("TARGET_REMINDER_CHANNEL"))
	reminderDelays := getAndParseDelayMins("REMINDER_BEFORES")
	reminderPrefix := buildReminderPrefix("REMINDER_TEXT", guildId)

	var translater Translater
	feedNumber := len(feedURLs)
	if deepLToken := strings.TrimSpace(os.Getenv("DEEPL_TOKEN")); deepLToken != "" && feedNumber != 0 {
		deepLUrl := requireConf("DEEPL_API_URL")
		sourceLang := strings.TrimSpace(os.Getenv("TRANSLATE_SOURCE_LANG"))
		targetLang := requireConf("TRANSLATE_TARGET_LANG")
		messageError := requireConf("MESSAGE_TRANSLATE_ERROR")
		messageLimit := requireConf("MESSAGE_TRANSLATE_LIMIT")
		translater = makeDeepLClient(deepLUrl, deepLToken, sourceLang, targetLang, messageError, messageLimit)
	}

	cmds := make([]*discordgo.ApplicationCommand, 0, len(cmdToRoleName)+4)
	applyName, cmds := appendCommand(cmds, "APPLY_CMD", "DESCRIPTION_APPLY_CMD")
	cleanName, cmds := appendCommand(cmds, "CLEAN_CMD", "DESCRIPTION_CLEAN_CMD")
	resetName, cmds := appendCommand(cmds, "RESET_CMD", "DESCRIPTION_RESET_CMD")
	countName, cmds := appendCommand(cmds, "COUNT_CMD", "DESCRIPTION_COUNT_CMD")
	roleCmdDesc := requireConf("DESCRIPTION_ROLE_CMD") + " "

	session, err := discordgo.New("Bot " + requireConf("BOT_TOKEN"))
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
	// for GC cleaning
	guild = nil

	guildChannels, err := session.GuildChannels(guildId)
	if err != nil {
		log.Println("Cannot retrieve the guild channels :", err)
		return // to allow defer
	}

	targetPrefixChannelId := ""
	targetCmdChannelId := ""
	targetNewsChannelId := ""
	targetReminderChannelId := ""
	for _, channel := range guildChannels {
		// multiple if with no else statement (could be the same channel)
		channelName := channel.Name
		if channelName == targetPrefixChannelName {
			targetPrefixChannelId = channel.ID
		}
		if channelName == targetCmdChannelName {
			targetCmdChannelId = channel.ID
		}
		if channelName == targetNewsChannelName {
			targetNewsChannelId = channel.ID
		}
		if channelName == targetReminderChannelName {
			targetReminderChannelId = channel.ID
		}
	}
	if targetNewsChannelId == "" && feedNumber != 0 {
		log.Println("Cannot retrieve the guild channel for news :", targetNewsChannelName)
		return // to allow defer
	}
	if targetReminderChannelId == "" {
		log.Println("Cannot retrieve the guild channel for reminders :", targetReminderChannelName)
		return // to allow defer
	}
	if targetPrefixChannelId == "" {
		log.Println("Cannot retrieve the guild channel, messages on nickname update will be disabled :", targetPrefixChannelName)
	}
	if targetCmdChannelId == "" {
		log.Println("Cannot retrieve the guild channel for background command messages :", targetCmdChannelName)
		return // to allow defer
	}
	// for GC cleaning
	guildChannels = nil
	targetPrefixChannelName = ""
	targetCmdChannelName = ""
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
	// for GC cleaning
	roleNameToPrefix = nil
	guildRoles = nil

	cmdRoleIds := map[string]empty{}
	cmdToRoleId := map[string]string{}
	for cmd, roleName := range cmdToRoleName {
		roleId := roleNameToId[roleName]
		if roleId == "" {
			log.Println("Unrecognized role name :", roleName)
			return // to allow defer
		}
		cmds = append(cmds, &discordgo.ApplicationCommand{
			Name:        cmd,
			Description: roleCmdDesc + roleName,
		})
		cmdToRoleId[cmd] = roleId
	}
	// for GC cleaning
	cmdToRoleName = nil

	authorizedRoleIds := initIdSet("AUTHORIZED_ROLES", roleNameToId)
	forbiddenRoleIds := initIdSet("FORBIDDEN_ROLES", roleNameToId)

	joiningRoleId := roleNameToId[joiningRole]
	// for GC cleaning
	joiningRole = ""

	defaultRoleId := roleNameToId[defaultRole]
	// for GC cleaning
	defaultRole = ""

	ignoredRoleIds := initIdSet("IGNORED_ROLES", roleNameToId)
	// for GC cleaning

	specialRoleIds := map[string]empty{}
	for _, name := range specialRoles {
		specialRoleIds[roleNameToId[name]] = empty{}
	}
	// for GC cleaning
	specialRoles = nil

	prefixRoleIds := map[string]empty{}
	for roleId := range roleIdToPrefix {
		prefixRoleIds[roleId] = empty{}
		if _, ok := specialRoleIds[roleId]; !ok {
			cmdRoleIds[roleId] = empty{}
		}
	}

	countFilter := false
	var countFilterRoleIds map[string]empty
	switch countFilterType := strings.TrimSpace(os.Getenv("COUNT_FILTER_TYPE")); countFilterType {
	case "list":
		countFilter = true
		countFilterRoleIds = initIdSet("COUNT_FILTER_ROLES", roleNameToId)
	case "prefix":
		countFilter = true
		countFilterRoleIds = prefixRoleIds
	case "cmdPrefix":
		countFilter = true
		countFilterRoleIds = cmdRoleIds
	}
	// for GC cleaning
	roleNameToId = nil
	prefixRoleIds = nil

	guildMembers, err := session.GuildMembers(guildId, "", 1000)
	if err != nil {
		log.Println("Cannot retrieve members of the guild :", err)
		return // to allow defer
	}

	channelManager := ChannelSenderManager{}
	channelManager.AddChannel(session, targetPrefixChannelId)
	channelManager.AddChannel(session, targetCmdChannelId)
	channelManager.AddChannel(session, targetNewsChannelId)
	channelManager.AddChannel(session, targetReminderChannelId)
	prefixChannelSender := channelManager[targetPrefixChannelId]
	cmdChannelSender := channelManager[targetCmdChannelId]

	var cmdMonitor Monitor
	counterError := applyPrefixes(session, guildMembers, guildId, ownerId, defaultRoleId, ignoredRoleIds, specialRoleIds, forbiddenRoleIds, roleIdToPrefix, prefixes, msgs)
	if counterError != 0 {
		log.Println("Trying to apply-prefix at startup generate errors :", counterError)
	}
	// for GC cleaning
	guildMembers = nil

	session.AddHandler(func(s *discordgo.Session, r *discordgo.GuildMemberAdd) {
		if err := s.GuildMemberRoleAdd(guildId, r.User.ID, joiningRoleId); err != nil {
			log.Println("Joining role addition failed :", err)
		}
	})

	session.AddHandler(func(s *discordgo.Session, u *discordgo.GuildMemberUpdate) {
		if userId := u.User.ID; userId != ownerId && !cmdMonitor.Running() {
			// messageSender can be non nil, so beforeUpdate must not be nil
			applyPrefix(s, prefixChannelSender, u.Member, u.BeforeUpdate, guildId, ownerId, defaultRoleId, ignoredRoleIds, specialRoleIds, forbiddenRoleIds, roleIdToPrefix, prefixes, msgs)
		}
	})

	defaultRoleDisplayName := roleIdToDisplayName[defaultRoleId]
	execCmds := map[string]func(*discordgo.Session, *discordgo.InteractionCreate){}
	addNonEmpty(execCmds, applyName, func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		membersCmd(s, cmdChannelSender, applyName, guildId, idInSet(i.Member.Roles, authorizedRoleIds), msgs, i.Interaction, &cmdMonitor, func(guildMembers []*discordgo.Member) int {
			return applyPrefixes(s, guildMembers, guildId, ownerId, defaultRoleId, ignoredRoleIds, specialRoleIds, forbiddenRoleIds, roleIdToPrefix, prefixes, msgs)
		})
	})
	addNonEmpty(execCmds, cleanName, func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		membersCmd(s, cmdChannelSender, cleanName, guildId, idInSet(i.Member.Roles, authorizedRoleIds), msgs, i.Interaction, &cmdMonitor, func(guildMembers []*discordgo.Member) int {
			return cleanPrefixes(s, guildMembers, guildId, ownerId, prefixes)
		})
	})
	addNonEmpty(execCmds, resetName, func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		addRoleCmd(s, i, ownerId, defaultRoleId, defaultRoleDisplayName, forbiddenRoleIds, cmdRoleIds, &cmdMonitor, msgs)
	})
	addNonEmpty(execCmds, countName, func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		countRoleCmd(s, i, roleIdToDisplayName, countFilter, countFilterRoleIds, msgs)
	})

	for cmd, roleId := range cmdToRoleId {
		addedRoleId := roleId
		addedRoleDisplayName := roleIdToDisplayName[addedRoleId]
		execCmds[cmd] = func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			addRoleCmd(s, i, ownerId, addedRoleId, addedRoleDisplayName, forbiddenRoleIds, cmdRoleIds, &cmdMonitor, msgs)
		}
	}
	// for GC cleaning
	cmdToRoleId = nil

	session.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if execCmd, ok := execCmds[i.ApplicationCommandData().Name]; ok {
			execCmd(s, i)
		}
	})

	appId := session.State.User.ID
	for index, cmd := range cmds {
		if cmds[index], err = session.ApplicationCommandCreate(appId, guildId, cmd); err != nil {
			log.Println("Cannot create", cmd.Name, "command :", err)
		}
	}

	go updateGameStatus(session, gameList, updateGameInterval)

	tickers := launchTickers(feedNumber+1, checkInterval)

	startTime := time.Now().Add(-checkInterval).Add(-getAndParseDurationSec("INITIAL_BACKWARD_LOADING"))
	bgReadMultipleRSS(channelManager[targetNewsChannelId], feedURLs, translater, startTime, tickers)
	go remindEvent(session, guildId, reminderDelays, channelManager[targetReminderChannelId], reminderPrefix, startTime, tickers[feedNumber])

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	log.Println("Press Ctrl+C to exit")
	<-stop

	for _, cmd := range cmds {
		if err = session.ApplicationCommandDelete(appId, guildId, cmd.ID); err != nil {
			log.Println("Cannot delete", cmd.Name, "command :", err)
		}
	}
}
