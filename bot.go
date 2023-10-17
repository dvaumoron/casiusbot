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
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/dvaumoron/casiusbot/common"
	"github.com/dvaumoron/casiusbot/deepl"
	"github.com/dvaumoron/casiusbot/gdrive"
)

func main() {
	defer common.LogBeforeShutdown()

	config := common.ReadConfig()
	roleNameToPrefix, prefixes, cmdAndRoleNames, specialRoles := config.GetPrefixConfig()

	errGlobalCmdMsg := config.GetString("MESSAGE_CMD_GLOBAL_ERROR")
	errPartialCmdMsg := config.GetString("MESSAGE_CMD_PARTIAL_ERROR")
	msgs := common.Messages{
		Ok:              config.GetString("MESSAGE_CMD_OK"),
		ErrUnauthorized: common.BuildMsgWithNameValueList(config.GetString("MESSAGE_CMD_UNAUTHORIZED"), roleNameToPrefix),
		ErrGlobalCmd:    errGlobalCmdMsg,
		ErrPartialCmd:   errPartialCmdMsg,
		Count:           config.GetString("MESSAGE_CMD_COUNT"),
		Prefix:          config.GetString("MESSAGE_PREFIX"),
		NoChange:        config.GetString("MESSAGE_NO_CHANGE"),
		EndedCmd:        config.GetString("MESSAGE_CMD_ENDED"),
		Owner:           config.GetString("MESSAGE_OWNER"),
		ErrGlobal:       common.CleanMessage(errGlobalCmdMsg),
		ErrPartial:      common.CleanMessage(errPartialCmdMsg),
	}

	guildId := config.Require("GUILD_ID")
	chatReponsePath, keywordToResponse := config.GetChatResponsesConfig()
	var keywordToResponseMutex sync.RWMutex

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
		panic("CHECK_INTERVAL is required")
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

	cmdConfig := config.GetCommandConfig()

	cmds := make([]*discordgo.ApplicationCommand, 0, len(cmdAndRoleNames)+8)
	applyName, cmds := common.AppendCommand(cmds, cmdConfig["APPLY"], nil)
	cleanName, cmds := common.AppendCommand(cmds, cmdConfig["CLEAN"], nil)
	resetName, cmds := common.AppendCommand(cmds, cmdConfig["RESET"], nil)
	resetAllName, cmds := common.AppendCommand(cmds, cmdConfig["RESET_ALL"], nil)
	countName, cmds := common.AppendCommand(cmds, cmdConfig["COUNT"], nil)
	roleCmdDesc := config.Require("DESCRIPTION_ROLE_CMD")

	userActivitiesName := ""
	monitorActivity := activityPath != "" && saveActivityInterval > 0
	if monitorActivity {
		userActivitiesName, cmds = common.AppendCommand(cmds, cmdConfig["USER_ACTIVITIES"], nil)
	}

	session, err := discordgo.New("Bot " + config.Require("BOT_TOKEN"))
	if err != nil {
		panic(fmt.Sprint("Cannot create the bot :", err))
	}
	session.Identify.Intents |= discordgo.IntentGuildMembers

	err = session.Open()
	if err != nil {
		panic(fmt.Sprint("Cannot open the session :", err))
	}
	defer session.Close()

	botId := session.State.Application.ID
	log.Println("botId", botId)

	guild, err := session.Guild(guildId)
	if err != nil {
		panic(fmt.Sprint("Cannot retrieve the guild :", err))
	}
	ownerId := guild.OwnerID
	guildRoles := guild.Roles
	// for GC cleaning
	guild = nil

	guildChannels, err := session.GuildChannels(guildId)
	if err != nil {
		panic(fmt.Sprint("Cannot retrieve the guild channels :", err))
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
		panic("Cannot retrieve the guild channel for reminders : " + targetReminderChannelName)
	}
	if targetPrefixChannelId == "" && targetPrefixChannelName != "" {
		panic("Cannot retrieve the guild channel for nickname update messages : " + targetPrefixChannelName)
	}
	if targetCmdChannelId == "" && (applyName != "" || cleanName != "" || resetAllName != "") {
		panic("Cannot retrieve the guild channel for background command messages : " + targetCmdChannelName)
	}
	if targetNewsChannelId == "" && feedActived {
		panic("Cannot retrieve the guild channel for news : " + targetNewsChannelName)
	}
	if targetActivitiesChannelId == "" && userActivitiesName != "" {
		panic("Cannot retrieve the guild channel for activities : " + targetActivitiesChannelName)
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
			panic("Unrecognized role name : " + cmdAndRoleName[1])
		}
		_, cmds = common.AppendCommand(cmds, [2]string{
			cmdAndRoleName[0], strings.ReplaceAll(roleCmdDesc, common.RolePlaceHolder, roleIdToDisplayName[roleId]),
		}, nil)
		cmdRoleIds[roleId] = common.Empty{}
		cmdAndRoleIds = append(cmdAndRoleIds, [2]string{cmdAndRoleName[0], roleId})
	}
	// for GC cleaning
	cmdAndRoleNames = nil

	authorizedRoleIds := config.GetIdSet("AUTHORIZED_ROLES", roleNameToId)
	forbiddenRoleIds := config.GetIdSet("FORBIDDEN_ROLES", roleNameToId)
	ignoredRoleIds := config.GetIdSet("IGNORED_ROLES", roleNameToId)

	defaultRoleId := roleNameToId[defaultRole]
	if defaultRoleId == "" {
		panic("Unrecognized role name for default : " + defaultRole)
	}
	// for GC cleaning
	defaultRole = ""

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

	specialRoleIds := common.InitIdSet(specialRoles, roleNameToId)
	// for GC cleaning
	specialRoles = nil

	var countFilterRoleIds common.StringSet
	switch countFilterType := config.GetString("COUNT_FILTER_TYPE"); countFilterType {
	case "list":
		countFilterRoleIds = config.GetIdSet("COUNT_FILTER_ROLES", roleNameToId)
	case "prefix":
		countFilterRoleIds = prefixRoleIds
	case "cmdPrefix":
		countFilterRoleIds = cmdRoleIds
	case "":
		// a nil countFilterRoleIds disable filtering
	default:
		panic("COUNT_FILTER_TYPE must be empty or one of : \"list\", \"prefix\", \"cmdPrefix\"")
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

	guildMembers, err := session.GuildMembers(guildId, "", common.MemberCallLimit)
	if err != nil {
		panic(fmt.Sprint("Cannot retrieve guild members :", err))
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
		return applyPrefix(session, nil, false, infos, guildMember)
	})
	if counterError != 0 {
		log.Println("Trying to apply prefixes at startup generate errors :", counterError)
	}
	// for GC cleaning
	guildMembers = nil

	session.AddHandler(func(s *discordgo.Session, u *discordgo.GuildMemberUpdate) {
		if userId := u.User.ID; userId != ownerId && userMonitor.StartProcessing(userId) {
			defer userMonitor.StopProcessing(userId)
			applyPrefix(s, prefixChannelSender, false, infos, u.Member)
		}
	})

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

	driveTokenName := ""
	registerChatRuleName := ""
	var driveConfig gdrive.DriveConfig
	var saveChan chan bool
	if monitorActivity {
		credentialsPath := config.GetPath("DRIVE_CREDENTIALS_PATH")
		tokenPath := config.GetPath("DRIVE_TOKEN_PATH")

		if credentialsPath != "" && tokenPath != "" {
			driveFolderId := config.Require("DRIVE_FOLDER_ID")

			stringParam := []*discordgo.ApplicationCommandOption{{
				Type: discordgo.ApplicationCommandOptionString, Name: "code",
				Description: config.Require("PARAMETER_DESCRIPTION_DRIVE_TOKEN_CMD"), Required: true,
			}}
			driveTokenName, cmds = common.AppendCommand(cmds, cmdConfig["DRIVE_TOKEN"], stringParam)
			followLinkMsg := strings.ReplaceAll(config.Require("MESSAGE_FOLLOW_LINK"), common.CmdPlaceHolder, driveTokenName)

			driveConfig = gdrive.ReadConfig(credentialsPath, tokenPath, followLinkMsg)

			// wrap the channel sender (used for errors or refresh token links)
			activityFileSender = driveConfig.CreateDriveSender(driveFolderId, activityFileSender)
		}

		saveChan = make(chan bool)
		go common.SendTick(saveChan, saveActivityInterval)
		activitySender := bgManageActivity(session, saveChan, activityFileSender, activityPath, dateFormat, userActivitiesName, infos)

		stringParams := []*discordgo.ApplicationCommandOption{{
			Type: discordgo.ApplicationCommandOptionString, Name: "keyword",
			Description: config.Require("PARAMETER_DESCRIPTION_REGISTER_CHAT_RULE_CMD_1"), Required: true,
		}, {
			Type: discordgo.ApplicationCommandOptionString, Name: "response",
			Description: config.Require("PARAMETER_DESCRIPTION_REGISTER_CHAT_RULE_CMD_2"), Required: true,
		}}
		registerChatRuleName, cmds = common.AppendCommand(cmds, cmdConfig["REGISTER_CHAT_RULE"], stringParams)

		session.AddHandler(func(s *discordgo.Session, u *discordgo.MessageCreate) {
			if u.Member != nil && !common.IdInSet(u.Member.Roles, adminitrativeRoleIds) {
				activitySender <- memberActivity{userId: u.Author.ID, timestamp: time.Now()}
			}

			manageChatResponse(s, u, botId, channelManager, keywordToResponse, &keywordToResponseMutex)
		})

		session.AddHandler(func(s *discordgo.Session, u *discordgo.VoiceStateUpdate) {
			if u.Member != nil && !common.IdInSet(u.Member.Roles, adminitrativeRoleIds) {
				activitySender <- memberActivity{userId: u.UserID, timestamp: time.Now(), vocal: true}
			}
		})
	}

	execCmds := map[string]func(*discordgo.Session, *discordgo.InteractionCreate){}
	applyMsgs := msgs.ReplaceCmdPlaceHolder(applyName)
	common.AddNonEmpty(execCmds, applyName, func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		common.MembersCmd(s, i, cmdChannelSender, infos, applyMsgs, &userMonitor, func(guildMember *discordgo.Member) int {
			return applyPrefix(s, nil, false, infos, guildMember)
		})
	})
	cleanMsgs := msgs.ReplaceCmdPlaceHolder(cleanName)
	common.AddNonEmpty(execCmds, cleanName, func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		common.MembersCmd(s, i, cmdChannelSender, infos, cleanMsgs, &userMonitor, func(guildMember *discordgo.Member) int {
			return cleanPrefix(s, infos, guildMember)
		})
	})
	common.AddNonEmpty(execCmds, resetName, func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		addRoleCmd(s, i, defaultRoleId, infos, &userMonitor)
	})
	resetAllMsgs := msgs.ReplaceCmdPlaceHolder(resetAllName)
	common.AddNonEmpty(execCmds, resetAllName, func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		common.MembersCmd(s, i, cmdChannelSender, infos, resetAllMsgs, &userMonitor, func(guildMember *discordgo.Member) int {
			return resetRole(s, infos, guildMember)
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
		common.AuthorizedCmd(s, i, infos, func() string {
			saveChan <- true
			return msgs.Ok
		})
	})
	common.AddNonEmpty(execCmds, driveTokenName, func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		driveConfig.DriveTokenCmd(s, i, infos)
	})
	common.AddNonEmpty(execCmds, registerChatRuleName, func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		registerChatResponseCmd(s, i, chatReponsePath, keywordToResponse, &keywordToResponseMutex, infos)
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
