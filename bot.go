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
	"bufio"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/dvaumoron/casiusbot/cache"
	"github.com/joho/godotenv"
	"github.com/mmcdole/gofeed"
)

const guildChannelNotFoundMsg = "Cannot retrieve the guild channel :"
const sendingFailedMsg = "Message sending failed :"

type empty = struct{}

func main() {
	if godotenv.Overload() == nil {
		log.Println("Loaded .env file")
	}

	okCmdMsg := os.Getenv("MESSAGE_CMD_OK")
	errPartialCmdMsg := os.Getenv("MESSAGE_CMD_PARTIAL_ERROR")
	errGlobalCmdMsg := os.Getenv("MESSAGE_CMD_GLOBAL_ERROR")
	errUnauthorizedCmdMsg := os.Getenv("MESSAGE_CMD_UNAUTHORIZED")

	guildId := os.Getenv("GUILD_ID")
	cmdRoles := strings.Split(os.Getenv("ROLES_CMD"), ",")
	defaultRole := os.Getenv("DEFAULT_ROLE")
	ignoredRoles := strings.Split(os.Getenv("IGNORED_ROLES"), ",")
	specialRoles := strings.Split(os.Getenv("SPECIAL_ROLES"), ",")
	targetNewsChannelName := os.Getenv("TARGET_NEWS_CHANNEL")
	feedURLs := getAndTrimSlice("FEED_URLS")
	rssStartInterval := getAndParseDurationSec("RSS_START_INTERVAL")
	rssReadInterval := getAndParseDurationSec("RSS_READ_INTERVAL")
	targetReminderChannelName := os.Getenv("TARGET_REMINDER_CHANNEL")
	reminderDelays := getAndParseDelayMins("REMINDER_BEFORES")
	var reminderBuilder strings.Builder
	reminderBuilder.WriteString(os.Getenv("REMINDER_TEXT"))
	reminderBuilder.WriteString("\nhttps://discord.com/events/")
	reminderBuilder.WriteString(guildId)
	reminderBuilder.WriteByte('/')
	reminderText := reminderBuilder.String()

	roleNameToPrefix, prefixes, err := readPrefixConfig()
	if err != nil {
		log.Fatalln("Cannot open the configuration file :", err)
	}

	applyCmd := &discordgo.ApplicationCommand{
		Name:        "apply-prefix",
		Description: "Apply the prefix rule to all User",
	}
	cleanCmd := &discordgo.ApplicationCommand{
		Name:        "clean-prefix",
		Description: "Clean the prefix for all User",
	}

	errUnauthorizedCmdMsg = buildNiceMsg(errUnauthorizedCmdMsg, roleNameToPrefix)

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
		log.Println("Cannot retrieve owner of the guild :", err)
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
		switch channel.Name {
		case targetNewsChannelName:
			targetNewsChannelId = channel.ID
		case targetReminderChannelName:
			targetReminderChannelId = channel.ID
		}
	}
	if targetNewsChannelId == "" {
		log.Println(guildChannelNotFoundMsg, targetNewsChannelName)
		return
	}
	if targetReminderChannelId == "" {
		log.Println(guildChannelNotFoundMsg, targetReminderChannelName)
		return
	}
	// emptying data no longer useful for GC cleaning
	guildChannels = nil
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

	counterError := applyPrefixes(session, guildMembers, guildId, ownerId, defaultRoleId, ignoredRoleIds, specialRoleIds, roleIdToPrefix, prefixes)
	if counterError != 0 {
		log.Println("Trying to apply-prefix at startup generate errors :", counterError)
	}

	var mutex sync.RWMutex
	cmdworking := false

	session.AddHandler(func(s *discordgo.Session, u *discordgo.GuildMemberUpdate) {
		if userId := u.User.ID; userId != ownerId {
			mutex.RLock()
			cmdworking2 := cmdworking
			mutex.RUnlock()

			if !cmdworking2 {
				applyPrefix(s, u.Member, u.GuildID, ownerId, defaultRoleId, ignoredRoleIds, specialRoleIds, roleIdToPrefix, prefixes)
			}
		}
	})

	session.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		switch i.ApplicationCommandData().Name {
		case applyCmd.Name:
			returnMsg := okCmdMsg
			if roleIdInSet(i.Member.Roles, cmdRoleIds) {
				mutex.Lock()
				cmdworking = true
				mutex.Unlock()

				guildMembers, err := s.GuildMembers(i.GuildID, "", 1000)
				if err == nil {
					counterError := applyPrefixes(s, guildMembers, i.GuildID, ownerId, defaultRoleId, ignoredRoleIds, specialRoleIds, roleIdToPrefix, prefixes)
					if counterError != 0 {
						returnMsg = buildPartialErrorString(errPartialCmdMsg, counterError)
					}
				} else {
					log.Println("Members retrieving failed :", err)
					returnMsg = errGlobalCmdMsg
				}

				mutex.Lock()
				cmdworking = false
				mutex.Unlock()
			} else {
				returnMsg = errUnauthorizedCmdMsg
			}

			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{Content: returnMsg},
			})
		case cleanCmd.Name:
			returnMsg := okCmdMsg
			if roleIdInSet(i.Member.Roles, cmdRoleIds) {
				mutex.Lock()
				cmdworking = true
				mutex.Unlock()

				guildMembers, err := s.GuildMembers(i.GuildID, "", 1000)
				if err == nil {
					counterError := 0
					for _, guildMember := range guildMembers {
						if userId := guildMember.User.ID; userId != ownerId {
							nickName := guildMember.Nick
							if nickName == "" {
								nickName = guildMember.User.Username
							}

							newNickName := cleanPrefix(nickName, prefixes)
							if newNickName != nickName {
								if err = s.GuildMemberNickname(i.GuildID, guildMember.User.ID, newNickName); err != nil {
									log.Println("Nickname change failed :", err)
									counterError++
								}
							}
						}
					}

					if counterError != 0 {
						returnMsg = buildPartialErrorString(errPartialCmdMsg, counterError)
					}
				} else {
					log.Println("Members retrieving failed (2) :", err)
					returnMsg = errGlobalCmdMsg
				}

				mutex.Lock()
				cmdworking = false
				mutex.Unlock()
			} else {
				returnMsg = errUnauthorizedCmdMsg
			}

			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{Content: returnMsg},
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

	messageChan := make(chan string)
	go sendMessage(session, targetNewsChannelId, messageChan)

	go updateGameStatus(session, getAndTrimSlice("GAME_LIST"))
	bgReadMultipleRSS(messageChan, feedURLs, time.Now().Add(-rssStartInterval), rssReadInterval)
	bgRemindEvent(session, guildId, reminderDelays, targetReminderChannelId, reminderText, rssReadInterval)
	bgServeHttp()

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

func readPrefixConfig() (map[string]string, []string, error) {
	file, err := os.Open(os.Getenv("PREFIX_FILE_PATH"))
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	nameToPrefix := map[string]string{}
	prefixes := make([]string, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) != 0 && line[0] != '#' {
			sepIndex := strings.IndexByte(line, '=')
			prefix := strings.TrimSpace(line[sepIndex+1:]) + " "
			nameToPrefix[strings.TrimSpace(line[:sepIndex])] = prefix
			prefixes = append(prefixes, prefix)
		}
	}
	return nameToPrefix, prefixes, nil
}

func transformName(nickName string, roleIds []string, defaultRoleId string, specialRoleIds map[string]empty, roleIdToPrefix map[string]string, prefixes []string) (string, bool, bool) {
	cleanedNickName := cleanPrefix(nickName, prefixes)
	nickName = cleanedNickName
	hasDefault, hasPrefix, notDone := false, false, true
	for _, roleId := range roleIds {
		if roleId == defaultRoleId {
			hasDefault = true
		}
		if prefix, ok := roleIdToPrefix[roleId]; ok {
			hasPrefix = true
			_, special := specialRoleIds[roleId]
			if notDone || special {
				notDone = false
				// prefix already end with a space
				nickName = prefix + cleanedNickName
			}
		}
	}
	return nickName, hasDefault, hasPrefix
}

func cleanPrefix(nickName string, prefixes []string) string {
	for _, prefix := range prefixes {
		if strings.HasPrefix(nickName, prefix) {
			return nickName[len(prefix):]
		}
	}
	return nickName
}

func roleIdInSet(roleIds []string, roleIdSet map[string]empty) bool {
	for _, roleId := range roleIds {
		if _, ok := roleIdSet[roleId]; ok {
			return true
		}
	}
	return false
}

func applyPrefixes(s *discordgo.Session, guildMembers []*discordgo.Member, guildId string, ownerId string, defaultRoleId string, ignoredRoleIds map[string]empty, specialRoleIds map[string]empty, roleIdToPrefix map[string]string, prefixes []string) int {
	counterError := 0
	for _, guildMember := range guildMembers {
		counterError += applyPrefix(s, guildMember, guildId, ownerId, defaultRoleId, ignoredRoleIds, specialRoleIds, roleIdToPrefix, prefixes)
	}
	return counterError
}

func applyPrefix(s *discordgo.Session, guildMember *discordgo.Member, guildId string, ownerId string, defaultRoleId string, ignoredRoleIds map[string]empty, specialRoleIds map[string]empty, roleIdToPrefix map[string]string, prefixes []string) int {
	counterError := 0
	userId := guildMember.User.ID
	roleIds := guildMember.Roles
	if userId != ownerId && !roleIdInSet(roleIds, ignoredRoleIds) {
		nickName := guildMember.Nick
		if nickName == "" {
			nickName = guildMember.User.Username
		}

		newNickName, hasDefault, hasPrefix := transformName(nickName, roleIds, defaultRoleId, specialRoleIds, roleIdToPrefix, prefixes)
		if hasDefault {
			if hasPrefix {
				if err := s.GuildMemberRoleRemove(guildId, userId, defaultRoleId); err != nil {
					log.Println("Role removing failed :", err)
					counterError++
				}
			}
		} else if !hasPrefix {
			if err := s.GuildMemberRoleAdd(guildId, userId, defaultRoleId); err != nil {
				log.Println("Role addition failed :", err)
				counterError++
			}
		}
		if newNickName != nickName {
			if err := s.GuildMemberNickname(guildId, userId, newNickName); err != nil {
				log.Println("Nickname change failed (2) :", err)
				counterError++
			}
		}
	}
	return counterError
}

func buildPartialErrorString(s string, i int) string {
	var buffer strings.Builder
	buffer.WriteString(s)
	buffer.WriteByte(' ')
	buffer.WriteString(strconv.Itoa(i))
	return buffer.String()
}

type namePrefixSortByName [][2]string

func (nps namePrefixSortByName) Len() int {
	return len(nps)
}

func (nps namePrefixSortByName) Less(i, j int) bool {
	return nps[i][0] < nps[j][0]
}

func (nps namePrefixSortByName) Swap(i, j int) {
	tmp := nps[i]
	nps[i] = nps[j]
	nps[j] = tmp
}

func buildNiceMsg(baseMsg string, roleNameToPrefix map[string]string) string {
	var buffer strings.Builder
	buffer.WriteString(baseMsg)
	namePrefixes := make([][2]string, 0, len(roleNameToPrefix))
	for name, prefix := range roleNameToPrefix {
		namePrefixes = append(namePrefixes, [2]string{name, prefix})
	}
	sort.Sort(namePrefixSortByName(namePrefixes))
	for _, namePrefix := range namePrefixes {
		buffer.WriteByte('\n')
		buffer.WriteString(namePrefix[0])
		buffer.WriteString(" = ")
		buffer.WriteString(namePrefix[1])
	}
	return buffer.String()
}

func initSetId(names []string, nameToId map[string]string) map[string]empty {
	setIds := map[string]empty{}
	for _, name := range names {
		setIds[nameToId[strings.TrimSpace(name)]] = empty{}
	}
	return setIds
}

func getAndTrimSlice(valuesName string) []string {
	values := strings.Split(os.Getenv(valuesName), ",")
	for index, value := range values {
		values[index] = strings.TrimSpace(value)
	}
	return values
}

func getAndParseDurationSec(valueName string) time.Duration {
	valueSec, err := strconv.ParseInt(os.Getenv(valueName), 10, 64)
	if err != nil {
		log.Fatalln("Configuration", valueName, "parsing failed :", err)
	}
	return time.Duration(valueSec) * time.Second
}

func getAndParseDelayMins(valuesName string) []time.Duration {
	values := strings.Split(os.Getenv(valuesName), ",")
	delays := make([]time.Duration, 0, len(values))
	for _, value := range values {
		valueMin, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			log.Fatalln("Configuration", valuesName, "parsing failed :", err)
		}
		delay := time.Duration(valueMin) * time.Minute
		if delay > 0 {
			delay = -delay
		}
		delays = append(delays, delay)
	}
	return delays
}

func sendMessage(session *discordgo.Session, channelId string, messageReceiver <-chan string) {
	botUserId := session.State.User.ID
	botMessageCache := cache.Make(100)
	oldMessages, err := session.ChannelMessages(channelId, 100, "", "", "")
	if err == nil {
		botOldMessages := make([]string, 0, 100)
		for _, message := range oldMessages {
			if message.Author.ID == botUserId {
				botOldMessages = append(botOldMessages, message.Content)
			}
		}
		botMessageCache.Init(botOldMessages)
	} else {
		log.Println("Message retrieving failed :", err)
	}

	for message := range messageReceiver {
		if !botMessageCache.Contains(message) {
			_, err = session.ChannelMessageSend(channelId, message)
			if err == nil {
				botMessageCache.Add(message)
			} else {
				log.Println("Message sending failed :", err)
			}
		}
	}
}

func updateGameStatus(session *discordgo.Session, games []string) {
	gamesLen := len(games)
	for range time.Tick(10 * time.Second) {
		session.UpdateGameStatus(0, games[rand.Intn(gamesLen)])
	}
}

func bgReadMultipleRSS(messageSender chan<- string, feedURLs []string, startTime time.Time, interval time.Duration) {
	subTickers := make([]chan time.Time, 0, len(feedURLs))
	for range feedURLs {
		subTickers = append(subTickers, make(chan time.Time, 1))
	}
	go startDispatchTick(startTime, interval, subTickers)

	for index, feedURL := range feedURLs {
		go startReadRSS(messageSender, feedURL, subTickers[index])
	}
}

func startDispatchTick(oldTime time.Time, interval time.Duration, subTickers []chan time.Time) {
	dispatchTick(oldTime, subTickers)
	oldTime = time.Now()
	for newTime := range time.Tick(interval) {
		dispatchTick(oldTime, subTickers)
		oldTime = newTime
	}
}

func dispatchTick(oldTime time.Time, subTickers []chan time.Time) {
	for _, subTicker := range subTickers {
		subTicker <- oldTime
	}
}

func startReadRSS(messageSender chan<- string, feedURL string, subTicker <-chan time.Time) {
	fp := gofeed.NewParser()
	for after := range subTicker {
		readRSS(messageSender, fp, feedURL, after)
	}
}

func readRSS(messageSender chan<- string, fp *gofeed.Parser, feedURL string, after time.Time) {
	feed, err := fp.ParseURL(feedURL)
	if err == nil {
		for _, item := range feed.Items {
			published := item.PublishedParsed
			if !published.IsZero() && published.After(after) {
				messageSender <- item.Link
			}
		}
	} else {
		log.Println("RSS parsing failed :", err)
	}
}

func bgRemindEvent(session *discordgo.Session, guildId string, delays []time.Duration, channelId string, reminderText string, interval time.Duration) {
	minusInteval := -interval
	for current := range time.Tick(interval) {
		previous := current.Add(minusInteval)
		events, err := session.GuildScheduledEvents(guildId, false)
		if err != nil {
			log.Println("Cannot retrieve guild events :", err)
			continue
		}

		for _, event := range events {
			for _, delay := range delays {
				// delay  is already negative
				reminderTime := event.ScheduledStartTime.Add(delay)
				if reminderTime.After(previous) && reminderTime.Before(current) {
					message := reminderText + event.ID
					if _, err = session.ChannelMessageSend(channelId, message); err != nil {
						log.Println(sendingFailedMsg, err)
					}
					// don't test other delay
					break
				}
			}
		}
	}
}

func bgServeHttp() {
	http.HandleFunc("/", hello)
	go startHttp()
}

var helloData = []byte("Hello World !")

func hello(w http.ResponseWriter, r *http.Request) {
	w.Write(helloData)
}

func startHttp() {
	http.ListenAndServe(":8080", nil)
}
