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
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

func main() {
	config, err := readConfig()
	if err != nil {
		log.Println(err)
		return
	}

	roleNameToPrefix, prefixes, cmdAndRoleNames, specialRoles := config.getPrefixConfig()

	okCmdMsg := config.getString("MESSAGE_CMD_OK")
	errUnauthorizedCmdMsg := buildMsgWithNameValueList(config.getString("MESSAGE_CMD_UNAUTHORIZED"), roleNameToPrefix)
	errGlobalCmdMsg := config.getString("MESSAGE_CMD_GLOBAL_ERROR")
	errPartialCmdMsg := config.getString("MESSAGE_CMD_PARTIAL_ERROR")
	countCmdMsg := config.getString("MESSAGE_CMD_COUNT")
	prefixMsg := config.getString("MESSAGE_PREFIX")
	noChangeMsg := config.getString("MESSAGE_NO_CHANGE")
	endedCmdMsg := config.getString("MESSAGE_CMD_ENDED")
	ownerMsg := config.getString("MESSAGE_OWNER")
	msgs := [...]string{
		okCmdMsg, errUnauthorizedCmdMsg, errGlobalCmdMsg, errPartialCmdMsg, countCmdMsg, prefixMsg,
		noChangeMsg, endedCmdMsg, strings.ReplaceAll(errPartialCmdMsg, cmdPlaceHolder+" ", ""), ownerMsg,
	}

	guildId := config.require("GUILD_ID")
	joiningRole := config.getString("JOINING_ROLE")
	defaultRole := config.require("DEFAULT_ROLE")
	gameList := config.getStringSlice("GAME_LIST")
	updateGameInterval := config.getDurationSec("UPDATE_GAME_INTERVAL")
	feeds := config.getSlice("FEEDS")
	checkInterval := config.getDurationSec("CHECK_INTERVAL")
	activityPath := config.getPath("ACTIVITY_FILE_PATH")
	saveActivityInterval := config.getDurationSec("SAVE_ACTIVITY_INTERVAL")
	dateFormat := config.getString("DATE_FORMAT")
	reminderDelays := config.getDelayMins("REMINDER_BEFORES")
	reminderPrefix := buildReminderPrefix(config, "REMINDER_TEXT", guildId)

	targetReminderChannelName := config.require("TARGET_REMINDER_CHANNEL")
	targetPrefixChannelName := config.getString("TARGET_PREFIX_CHANNEL")
	targetCmdChannelName := config.getString("TARGET_CMD_CHANNEL")
	targetNewsChannelName := ""
	targetActivitiesChannelName := config.getString("TARGET_ACTIVITIES_CHANNEL")

	if checkInterval == 0 {
		log.Fatalln("CHECK_INTERVAL is required")
	}

	var translater Translater
	feedNumber := len(feeds)
	feedActived := feedNumber != 0
	if feedActived {
		targetNewsChannelName = config.require("TARGET_NEWS_CHANNEL")
		if deepLToken := config.getString("DEEPL_TOKEN"); deepLToken != "" {
			deepLUrl := config.require("DEEPL_API_URL")
			sourceLang := config.getString("TRANSLATE_SOURCE_LANG")
			targetLang := config.require("TRANSLATE_TARGET_LANG")
			messageError := config.require("MESSAGE_TRANSLATE_ERROR")
			messageLimit := config.require("MESSAGE_TRANSLATE_LIMIT")
			translater = makeDeepLClient(deepLUrl, deepLToken, sourceLang, targetLang, messageError, messageLimit)
		}
	}

	monitorActivity := activityPath != "" && saveActivityInterval > 0

	cmds := make([]*discordgo.ApplicationCommand, 0, len(cmdAndRoleNames)+5)
	applyName, cmds := appendCommand(cmds, config, "APPLY_CMD", "DESCRIPTION_APPLY_CMD")
	cleanName, cmds := appendCommand(cmds, config, "CLEAN_CMD", "DESCRIPTION_CLEAN_CMD")
	resetName, cmds := appendCommand(cmds, config, "RESET_CMD", "DESCRIPTION_RESET_CMD")
	resetAllName, cmds := appendCommand(cmds, config, "RESET_ALL_CMD", "DESCRIPTION_RESET_ALL_CMD")
	countName, cmds := appendCommand(cmds, config, "COUNT_CMD", "DESCRIPTION_COUNT_CMD")
	roleCmdDesc := config.require("DESCRIPTION_ROLE_CMD")
	var userActivitiesName string
	if monitorActivity {
		userActivitiesName, cmds = appendCommand(cmds, config, "USER_ACTIVITIES_CMD", "DESCRIPTION_USER_ACTIVITIES_CMD")
	}

	session, err := discordgo.New("Bot " + config.require("BOT_TOKEN"))
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

	targetReminderChannelId := ""
	targetPrefixChannelId := ""
	targetCmdChannelId := ""
	targetNewsChannelId := ""
	targetActivitiesChannelId := ""
	for _, channel := range guildChannels {
		// multiple if with no else statement (could be the same channel)
		channelName := channel.Name
		if channelName == targetReminderChannelName {
			targetReminderChannelId = channel.ID
		}
		if channelName == targetPrefixChannelName {
			targetPrefixChannelId = channel.ID
		}
		if channelName == targetCmdChannelName {
			targetCmdChannelId = channel.ID
		}
		if channelName == targetNewsChannelName {
			targetNewsChannelId = channel.ID
		}
		if channelName == targetActivitiesChannelName {
			targetActivitiesChannelId = channel.ID
		}
	}
	if targetReminderChannelId == "" {
		log.Println("Cannot retrieve the guild channel for reminders :", targetReminderChannelName)
		return // to allow defer
	}
	if targetPrefixChannelId == "" && targetPrefixChannelName != "" {
		log.Println("Cannot retrieve the guild channel for nickname update messages :", targetPrefixChannelName)
		return // to allow defer
	}
	if targetCmdChannelId == "" && (applyName != "" || cleanName != "") {
		log.Println("Cannot retrieve the guild channel for background command messages :", targetCmdChannelName)
		return // to allow defer
	}
	if targetNewsChannelId == "" && feedActived {
		log.Println("Cannot retrieve the guild channel for news :", targetNewsChannelName)
		return // to allow defer
	}
	if targetActivitiesChannelId == "" && userActivitiesName != "" {
		log.Println("Cannot retrieve the guild channel for activities :", targetActivitiesChannelName)
		return // to allow defer
	}
	// for GC cleaning
	guildChannels = nil
	targetReminderChannelName = ""
	targetPrefixChannelName = ""
	targetCmdChannelName = ""
	targetNewsChannelName = ""
	targetActivitiesChannelName = ""

	roleNameToId := map[string]string{}
	prefixRoleIds := stringSet{}
	roleIdToPrefix := map[string]string{}
	roleIdToDisplayName := map[string]string{}
	for _, guildRole := range guildRoles {
		name := guildRole.Name
		id := guildRole.ID
		roleNameToId[name] = id
		displayName := name
		if prefix, ok := roleNameToPrefix[name]; ok {
			roleIdToPrefix[id] = prefix
			prefixRoleIds[id] = empty{}
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

	cmdRoleIds := stringSet{}
	cmdAndRoleIds := make([][2]string, 0, len(cmdAndRoleNames))
	for _, cmdAndRoleName := range cmdAndRoleNames {
		roleId := roleNameToId[cmdAndRoleName[1]]
		if roleId == "" {
			log.Println("Unrecognized role name :", cmdAndRoleName[1])
			return // to allow defer
		}
		cmds = append(cmds, &discordgo.ApplicationCommand{
			Name: cmdAndRoleName[0], Description: strings.ReplaceAll(roleCmdDesc, "{{role}}", roleIdToDisplayName[roleId]),
		})
		cmdRoleIds[roleId] = empty{}
		cmdAndRoleIds = append(cmdAndRoleIds, [2]string{cmdAndRoleName[0], roleId})
	}
	// for GC cleaning
	cmdAndRoleNames = nil

	authorizedRoleIds, err := config.getIdSet("AUTHORIZED_ROLES", roleNameToId)
	if err != nil {
		log.Println(err)
		return // to allow defer
	}
	forbiddenRoleIds, err := config.getIdSet("FORBIDDEN_ROLES", roleNameToId)
	if err != nil {
		log.Println(err)
		return // to allow defer
	}

	defaultRoleId := roleNameToId[defaultRole]
	if defaultRoleId == "" {
		log.Println("Unrecognized role name for default :", defaultRole)
		return // to allow defer
	}
	// for GC cleaning
	defaultRole = ""

	ignoredRoleIds, err := config.getIdSet("IGNORED_ROLES", roleNameToId)
	if err != nil {
		log.Println(err)
		return // to allow defer
	}

	// merge the two categories
	forbiddenAndignoredRoleIds := stringSet{}
	for roleId := range forbiddenRoleIds {
		forbiddenAndignoredRoleIds[roleId] = empty{}
	}
	for roleId := range ignoredRoleIds {
		forbiddenAndignoredRoleIds[roleId] = empty{}
	}

	adminitrativeRoleIds := stringSet{}
	for roleId := range forbiddenAndignoredRoleIds {
		adminitrativeRoleIds[roleId] = empty{}
	}
	for roleId := range authorizedRoleIds {
		adminitrativeRoleIds[roleId] = empty{}
	}

	specialRoleIds, err := initIdSet(specialRoles, roleNameToId)
	if err != nil {
		log.Println(err)
		return // to allow defer
	}
	// for GC cleaning
	specialRoles = nil

	var countFilterRoleIds stringSet
	switch countFilterType := config.getString("COUNT_FILTER_TYPE"); countFilterType {
	case "list":
		countFilterRoleIds, err = config.getIdSet("COUNT_FILTER_ROLES", roleNameToId)
		if err != nil {
			log.Println(err)
			return // to allow defer
		}
	case "prefix":
		countFilterRoleIds = prefixRoleIds
	case "cmdPrefix":
		countFilterRoleIds = cmdRoleIds
	case "":
		// a nil countFilterRoleIds disable filtering
	default:
		log.Println("COUNT_FILTER_TYPE must be empty or one of : \"list\", \"prefix\", \"cmdPrefix\"")
		return // to allow defer
	}
	// for GC cleaning
	roleNameToId = nil
	prefixRoleIds = nil

	infos := GuildAndConfInfo{
		guildId: guildId, ownerId: ownerId, defaultRoleId: defaultRoleId, authorizedRoleIds: authorizedRoleIds,
		forbiddenRoleIds: forbiddenRoleIds, ignoredRoleIds: ignoredRoleIds, forbiddenAndignoredRoleIds: forbiddenAndignoredRoleIds,
		adminitrativeRoleIds: adminitrativeRoleIds, cmdRoleIds: cmdRoleIds, specialRoleIds: specialRoleIds,
		roleIdToPrefix: roleIdToPrefix, prefixes: prefixes, roleIdToDisplayName: roleIdToDisplayName, msgs: msgs,
	}

	guildMembers, err := session.GuildMembers(guildId, "", 1000)
	if err != nil {
		log.Println("Cannot retrieve guild members :", err)
		return // to allow defer
	}

	channelManager := MakeChannelSenderManager(session)
	channelManager.AddChannel(targetPrefixChannelId)
	channelManager.AddChannel(targetCmdChannelId)
	channelManager.AddChannel(targetNewsChannelId)
	channelManager.AddChannel(targetReminderChannelId)
	prefixChannelSender := channelManager.Get(targetPrefixChannelId)
	cmdChannelSender := channelManager.Get(targetCmdChannelId)

	userMonitor := MakeIdMonitor()
	if counterError := applyPrefixes(session, guildMembers, infos, &userMonitor); counterError != 0 {
		log.Println("Trying to apply prefixes at startup generate errors :", counterError)
	}
	// for GC cleaning
	guildMembers = nil

	session.AddHandler(func(s *discordgo.Session, u *discordgo.GuildMemberUpdate) {
		if userId := u.User.ID; userId != ownerId && userMonitor.StartProcessing(userId) {
			defer userMonitor.StopProcessing(userId)
			applyPrefix(s, prefixChannelSender, u.Member, infos, false)
		}
	})

	var saveChan chan bool
	if monitorActivity {
		saveChan = make(chan bool)
		go sendTick(saveChan, saveActivityInterval)
		activitySender := bgManageActivity(session, saveChan, targetActivitiesChannelId, userActivitiesName, activityPath, dateFormat, infos)

		session.AddHandler(func(s *discordgo.Session, u *discordgo.MessageCreate) {
			if u.Member != nil && !idInSet(u.Member.Roles, adminitrativeRoleIds) {
				activitySender <- memberActivity{userId: u.Author.ID, timestamp: time.Now()}
			}
		})

		session.AddHandler(func(s *discordgo.Session, u *discordgo.VoiceStateUpdate) {
			if u.Member != nil && !idInSet(u.Member.Roles, adminitrativeRoleIds) {
				activitySender <- memberActivity{userId: u.UserID, timestamp: time.Now(), vocal: true}
			}
		})
	}

	if joiningRoleId := roleNameToId[joiningRole]; joiningRoleId != "" {
		// joining rule is after prefix rule, to manage case where joining role have a prefix
		session.AddHandler(func(s *discordgo.Session, r *discordgo.GuildMemberAdd) {
			if err := s.GuildMemberRoleAdd(guildId, r.User.ID, joiningRoleId); err != nil {
				log.Println("Joining role addition failed :", err)
			}
		})
	}
	// for GC cleaning
	joiningRole = ""

	execCmds := map[string]func(*discordgo.Session, *discordgo.InteractionCreate){}
	addNonEmpty(execCmds, applyName, func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		membersCmd(s, i, cmdChannelSender, applyName, infos, func(guildMembers []*discordgo.Member) int {
			return applyPrefixes(s, guildMembers, infos, &userMonitor)
		})
	})
	addNonEmpty(execCmds, cleanName, func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		membersCmd(s, i, cmdChannelSender, cleanName, infos, func(guildMembers []*discordgo.Member) int {
			return cleanPrefixes(s, guildMembers, infos, &userMonitor)
		})
	})
	addNonEmpty(execCmds, resetName, func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		addRoleCmd(s, i, defaultRoleId, infos, &userMonitor)
	})
	addNonEmpty(execCmds, resetAllName, func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		membersCmd(s, i, cmdChannelSender, resetAllName, infos, func(guildMembers []*discordgo.Member) int {
			return resetRoleAll(s, guildMembers, infos, &userMonitor)
		})
	})

	roleCountExtracter := extractRoleCount
	if len(countFilterRoleIds) != 0 {
		roleCountExtracter = func(guildMembers []*discordgo.Member) map[string]int {
			return extractRoleCountWithFilter(guildMembers, countFilterRoleIds)
		}
	}
	addNonEmpty(execCmds, countName, func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		countRoleCmd(s, i, roleCountExtracter, infos)
	})

	for _, cmdAndRoleId := range cmdAndRoleIds {
		addedRoleId := cmdAndRoleId[1]
		execCmds[cmdAndRoleId[0]] = func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			addRoleCmd(s, i, addedRoleId, infos, &userMonitor)
		}
	}
	// for GC cleaning
	cmdAndRoleIds = nil

	addNonEmpty(execCmds, userActivitiesName, func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		userActivitiesCmd(s, i, saveChan, infos)
	})

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

	startTime := time.Now().Add(-checkInterval)
	if backwardLoading := config.getDurationSec("INITIAL_BACKWARD_LOADING"); backwardLoading != 0 {
		startTime = startTime.Add(-backwardLoading)
	}

	bgReadMultipleRSS(channelManager.Get(targetNewsChannelId), feeds, translater, startTime, tickers)
	go remindEvent(session, guildId, reminderDelays, channelManager.Get(targetReminderChannelId), reminderPrefix, startTime, tickers[feedNumber])

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	log.Println("Started successfully")
	fmt.Println("Press Ctrl+C to exit")
	<-stop

	for _, cmd := range cmds {
		if err = session.ApplicationCommandDelete(appId, guildId, cmd.ID); err != nil {
			log.Println("Cannot delete", cmd.Name, "command :", err)
		}
	}
}
