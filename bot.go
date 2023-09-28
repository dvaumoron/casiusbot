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
	"github.com/dvaumoron/casiusbot/common"
	"github.com/dvaumoron/casiusbot/deepl"
	"github.com/dvaumoron/casiusbot/gdrive"
)

func main() {
	config, err := common.ReadConfig()
	if err != nil {
		log.Println(err)
		return
	}

	roleNameToPrefix, prefixes, cmdAndRoleNames, specialRoles := config.GetPrefixConfig()

	okCmdMsg := config.GetString("MESSAGE_CMD_OK")
	errUnauthorizedCmdMsg := common.BuildMsgWithNameValueList(config.GetString("MESSAGE_CMD_UNAUTHORIZED"), roleNameToPrefix)
	errGlobalCmdMsg := config.GetString("MESSAGE_CMD_GLOBAL_ERROR")
	errPartialCmdMsg := config.GetString("MESSAGE_CMD_PARTIAL_ERROR")
	countCmdMsg := config.GetString("MESSAGE_CMD_COUNT")
	prefixMsg := config.GetString("MESSAGE_PREFIX")
	noChangeMsg := config.GetString("MESSAGE_NO_CHANGE")
	endedCmdMsg := config.GetString("MESSAGE_CMD_ENDED")
	ownerMsg := config.GetString("MESSAGE_OWNER")
	msgs := [...]string{
		okCmdMsg, errUnauthorizedCmdMsg, errGlobalCmdMsg, errPartialCmdMsg,
		countCmdMsg, prefixMsg, noChangeMsg, endedCmdMsg, ownerMsg,
		common.CleanMessage(errGlobalCmdMsg), common.CleanMessage(errPartialCmdMsg),
	}

	guildId := config.Require("GUILD_ID")
	joiningRole := config.GetString("JOINING_ROLE")
	defaultRole := config.Require("DEFAULT_ROLE")
	gameList := config.GetStringSlice("GAME_LIST")
	updateGameInterval := config.GetDurationSec("UPDATE_GAME_INTERVAL")
	feeds := config.GetSlice("FEEDS")
	checkInterval := config.GetDurationSec("CHECK_INTERVAL")
	activityPath := config.GetPath("ACTIVITY_FILE_PATH")
	saveActivityInterval := config.GetDurationSec("SAVE_ACTIVITY_INTERVAL")
	dateFormat := config.GetString("DATE_FORMAT")
	reminderDelays := config.GetDelayMins("REMINDER_BEFORES")
	reminderPrefix := buildReminderPrefix(config, "REMINDER_TEXT", guildId)

	targetReminderChannelName := config.Require("TARGET_REMINDER_CHANNEL")
	targetPrefixChannelName := config.GetString("TARGET_PREFIX_CHANNEL")
	targetCmdChannelName := config.GetString("TARGET_CMD_CHANNEL")
	targetNewsChannelName := ""
	targetActivitiesChannelName := config.GetString("TARGET_ACTIVITIES_CHANNEL")

	if checkInterval == 0 {
		log.Fatalln("CHECK_INTERVAL is required")
	}

	var translater Translater
	feedNumber := len(feeds)
	feedActived := feedNumber != 0
	if feedActived {
		targetNewsChannelName = config.Require("TARGET_NEWS_CHANNEL")
		if deepLToken := config.GetString("DEEPL_TOKEN"); deepLToken != "" {
			deepLUrl := config.Require("DEEPL_API_URL")
			sourceLang := config.GetString("TRANSLATE_SOURCE_LANG")
			targetLang := config.Require("TRANSLATE_TARGET_LANG")
			messageError := config.Require("MESSAGE_TRANSLATE_ERROR")
			messageLimit := config.Require("MESSAGE_TRANSLATE_LIMIT")
			translater = deepl.MakeClient(deepLUrl, deepLToken, sourceLang, targetLang, messageError, messageLimit)
		}
	}

	monitorActivity := activityPath != "" && saveActivityInterval > 0
	credentialsPath := config.GetPath("DRIVE_CREDENTIALS_PATH")
	tokenPath := config.GetPath("DRIVE_TOKEN_PATH")
	driveFolderId := config.GetString("DRIVE_FOLDER_ID")

	cmds := make([]*discordgo.ApplicationCommand, 0, len(cmdAndRoleNames)+5)
	applyName, cmds := common.AppendCommand(cmds, config, "APPLY_CMD", "DESCRIPTION_APPLY_CMD", nil)
	cleanName, cmds := common.AppendCommand(cmds, config, "CLEAN_CMD", "DESCRIPTION_CLEAN_CMD", nil)
	resetName, cmds := common.AppendCommand(cmds, config, "RESET_CMD", "DESCRIPTION_RESET_CMD", nil)
	resetAllName, cmds := common.AppendCommand(cmds, config, "RESET_ALL_CMD", "DESCRIPTION_RESET_ALL_CMD", nil)
	countName, cmds := common.AppendCommand(cmds, config, "COUNT_CMD", "DESCRIPTION_COUNT_CMD", nil)
	roleCmdDesc := config.Require("DESCRIPTION_ROLE_CMD")

	userActivitiesName := ""

	if monitorActivity {
		userActivitiesName, cmds = common.AppendCommand(cmds, config, "USER_ACTIVITIES_CMD", "DESCRIPTION_USER_ACTIVITIES_CMD", nil)
	}

	session, err := discordgo.New("Bot " + config.Require("BOT_TOKEN"))
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
	prefixRoleIds := common.StringSet{}
	roleIdToPrefix := map[string]string{}
	roleIdToDisplayName := map[string]string{}
	for _, guildRole := range guildRoles {
		name := guildRole.Name
		id := guildRole.ID
		roleNameToId[name] = id
		displayName := name
		if prefix, ok := roleNameToPrefix[name]; ok {
			roleIdToPrefix[id] = prefix
			prefixRoleIds[id] = common.Empty{}
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

	cmdRoleIds := common.StringSet{}
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
		cmdRoleIds[roleId] = common.Empty{}
		cmdAndRoleIds = append(cmdAndRoleIds, [2]string{cmdAndRoleName[0], roleId})
	}
	// for GC cleaning
	cmdAndRoleNames = nil

	authorizedRoleIds, err := config.GetIdSet("AUTHORIZED_ROLES", roleNameToId)
	if err != nil {
		log.Println(err)
		return // to allow defer
	}
	forbiddenRoleIds, err := config.GetIdSet("FORBIDDEN_ROLES", roleNameToId)
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

	ignoredRoleIds, err := config.GetIdSet("IGNORED_ROLES", roleNameToId)
	if err != nil {
		log.Println(err)
		return // to allow defer
	}

	// merge the two categories
	forbiddenAndignoredRoleIds := common.StringSet{}
	for roleId := range forbiddenRoleIds {
		forbiddenAndignoredRoleIds[roleId] = common.Empty{}
	}
	for roleId := range ignoredRoleIds {
		forbiddenAndignoredRoleIds[roleId] = common.Empty{}
	}

	adminitrativeRoleIds := common.StringSet{}
	for roleId := range forbiddenAndignoredRoleIds {
		adminitrativeRoleIds[roleId] = common.Empty{}
	}
	for roleId := range authorizedRoleIds {
		adminitrativeRoleIds[roleId] = common.Empty{}
	}

	specialRoleIds, err := common.InitIdSet(specialRoles, roleNameToId)
	if err != nil {
		log.Println(err)
		return // to allow defer
	}
	// for GC cleaning
	specialRoles = nil

	var countFilterRoleIds common.StringSet
	switch countFilterType := config.GetString("COUNT_FILTER_TYPE"); countFilterType {
	case "list":
		countFilterRoleIds, err = config.GetIdSet("COUNT_FILTER_ROLES", roleNameToId)
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

	infos := common.GuildAndConfInfo{
		GuildId: guildId, OwnerId: ownerId, DefaultRoleId: defaultRoleId, AuthorizedRoleIds: authorizedRoleIds,
		ForbiddenRoleIds: forbiddenRoleIds, IgnoredRoleIds: ignoredRoleIds, ForbiddenAndignoredRoleIds: forbiddenAndignoredRoleIds,
		AdminitrativeRoleIds: adminitrativeRoleIds, CmdRoleIds: cmdRoleIds, SpecialRoleIds: specialRoleIds,
		RoleIdToPrefix: roleIdToPrefix, Prefixes: prefixes, RoleIdToDisplayName: roleIdToDisplayName, Msgs: msgs,
	}

	guildMembers, err := session.GuildMembers(guildId, "", 1000)
	if err != nil {
		log.Println("Cannot retrieve guild members :", err)
		return // to allow defer
	}

	channelManager := common.MakeChannelSenderManager(session)
	channelManager.AddChannel(targetPrefixChannelId)
	channelManager.AddChannel(targetCmdChannelId)
	channelManager.AddChannel(targetNewsChannelId)
	channelManager.AddChannel(targetReminderChannelId)
	channelManager.AddChannel(targetActivitiesChannelId)

	prefixChannelSender := channelManager.Get(targetPrefixChannelId)
	cmdChannelSender := channelManager.Get(targetCmdChannelId)
	activityFileSender := channelManager.Get(targetActivitiesChannelId)

	userMonitor := common.MakeIdMonitor()
	counterError := common.ProcessMembers(guildMembers, &userMonitor, func(guildMember *discordgo.Member) int {
		return applyPrefix(session, nil, guildMember, infos, false)
	})
	if counterError != 0 {
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

	driveTokenName := ""
	var driveConfig gdrive.DriveConfig
	var saveChan chan bool
	if monitorActivity {
		if credentialsPath != "" && tokenPath != "" && driveFolderId != "" {
			stringParam := []*discordgo.ApplicationCommandOption{{
				Type: discordgo.ApplicationCommandOptionString, Name: "code",
				Description: config.Require("PARAMETER_DESCRIPTION_DRIVE_TOKEN_CMD"), Required: true,
			}}
			driveTokenName, cmds = common.AppendCommand(cmds, config, "DRIVE_TOKEN_CMD", "DESCRIPTION_DRIVE_TOKEN_CMD", stringParam)
			followLinkMsg := strings.ReplaceAll(config.Require("MESSAGE_FOLLOW_LINK"), common.CmdPlaceHolder, driveTokenName)

			driveConfig, err = gdrive.ReadDriveConfig(credentialsPath, tokenPath, followLinkMsg)
			if err != nil {
				log.Println("Google Drive configuration initialization failed :", err)
				return // to allow defer
			}

			// wrap the channel sender (used for errors or refresh token links)
			activityFileSender = driveConfig.CreateDriveSender(driveFolderId, activityFileSender)
		}

		saveChan = make(chan bool)
		go common.SendTick(saveChan, saveActivityInterval)
		activitySender := bgManageActivity(session, saveChan, activityFileSender, activityPath, dateFormat, userActivitiesName, infos)

		session.AddHandler(func(s *discordgo.Session, u *discordgo.MessageCreate) {
			if u.Member != nil && !common.IdInSet(u.Member.Roles, adminitrativeRoleIds) {
				activitySender <- memberActivity{userId: u.Author.ID, timestamp: time.Now()}
			}
		})

		session.AddHandler(func(s *discordgo.Session, u *discordgo.VoiceStateUpdate) {
			if u.Member != nil && !common.IdInSet(u.Member.Roles, adminitrativeRoleIds) {
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
	common.AddNonEmpty(execCmds, applyName, func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		common.MembersCmd(s, i, cmdChannelSender, applyName, infos, &userMonitor, func(guildMember *discordgo.Member) int {
			return applyPrefix(s, nil, guildMember, infos, false)
		})
	})
	common.AddNonEmpty(execCmds, cleanName, func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		common.MembersCmd(s, i, cmdChannelSender, cleanName, infos, &userMonitor, func(guildMember *discordgo.Member) int {
			return cleanPrefix(s, guildMember, infos)
		})
	})
	common.AddNonEmpty(execCmds, resetName, func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		addRoleCmd(s, i, defaultRoleId, infos, &userMonitor)
	})
	common.AddNonEmpty(execCmds, resetAllName, func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		common.MembersCmd(s, i, cmdChannelSender, resetAllName, infos, &userMonitor, func(guildMember *discordgo.Member) int {
			return resetRole(s, guildMember, infos)
		})
	})

	roleCountExtracter := extractRoleCount
	if len(countFilterRoleIds) != 0 {
		roleCountExtracter = func(guildMembers []*discordgo.Member) map[string]int {
			return extractRoleCountWithFilter(guildMembers, countFilterRoleIds)
		}
	}
	common.AddNonEmpty(execCmds, countName, func(s *discordgo.Session, i *discordgo.InteractionCreate) {
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

	common.AddNonEmpty(execCmds, userActivitiesName, func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		userActivitiesCmd(s, i, saveChan, infos)
	})
	common.AddNonEmpty(execCmds, driveTokenName, func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		driveConfig.DriveTokenCmd(s, i, infos)
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

	go common.UpdateGameStatus(session, gameList, updateGameInterval)

	tickers := common.LaunchTickers(feedNumber+1, checkInterval)

	startTime := time.Now().Add(-checkInterval)
	if backwardLoading := config.GetDurationSec("INITIAL_BACKWARD_LOADING"); backwardLoading != 0 {
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
		if cmd != nil { // there is nil when the command creation failed
			if err = session.ApplicationCommandDelete(appId, guildId, cmd.ID); err != nil {
				log.Println("Cannot delete", cmd.Name, "command :", err)
			}
		}
	}
}
