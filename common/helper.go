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

package common

import (
	"cmp"
	"log"
	"math/rand"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/bwmarrin/discordgo"
)

// max number of members returned by Discord API
// (as stated in Session.GuildMembers documentation)
const MemberCallLimit = 1000

const (
	CmdPlaceHolder      = "{{cmd}}"
	NumErrorPlaceHolder = "{{numError}}"
	RolePlaceHolder     = "{{role}}"
)

type Empty = struct{}
type StringSet = map[string]Empty

type GuildAndConfInfo struct {
	GuildId                    string
	OwnerId                    string
	DefaultRoleId              string
	AuthorizedRoleIds          StringSet
	ForbiddenRoleIds           StringSet
	IgnoredRoleIds             StringSet
	ForbiddenAndignoredRoleIds StringSet
	AdminitrativeRoleIds       StringSet
	CmdRoleIds                 StringSet
	SpecialRoleIds             StringSet
	RoleIdToPrefix             map[string]string
	Prefixes                   []string
	RoleIdToDisplayName        map[string]string
	Msgs                       Messages
}

type Messages struct {
	Ok              string
	ErrUnauthorized string
	ErrGlobalCmd    string
	ErrPartialCmd   string
	Count           string
	Prefix          string
	NoChange        string
	EndedCmd        string
	Owner           string
	ErrGlobal       string
	ErrPartial      string
}

func (m Messages) ReplaceCmdPlaceHolder(cmdName string) Messages {
	if cmdName != "" {
		m.ErrGlobalCmd = strings.ReplaceAll(m.ErrGlobalCmd, CmdPlaceHolder, cmdName)
		m.ErrPartialCmd = strings.ReplaceAll(m.ErrPartialCmd, CmdPlaceHolder, cmdName)
		m.EndedCmd = strings.ReplaceAll(m.EndedCmd, CmdPlaceHolder, cmdName)
	}
	return m
}

type IdMonitor struct {
	processing StringSet
	mutex      sync.RWMutex
}

func MakeIdMonitor() IdMonitor {
	return IdMonitor{processing: StringSet{}}
}

func (m *IdMonitor) StopProcessing(id string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	delete(m.processing, id)
}

func (m *IdMonitor) StartProcessing(id string) bool {
	m.mutex.RLock()
	_, processing := m.processing[id]
	m.mutex.RUnlock()
	if processing {
		return false
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()
	// verify there was no change between lock
	_, processing = m.processing[id]
	if processing {
		return false
	}
	m.processing[id] = Empty{}
	return true
}

type MultipartMessage struct {
	Message    string
	FileName   string
	FileData   string
	ErrorMsg   string
	AllowMerge bool
}

type ChannelSenderManager struct {
	channels map[string]chan<- MultipartMessage
	session  *discordgo.Session
}

func MakeChannelSenderManager(session *discordgo.Session) ChannelSenderManager {
	return ChannelSenderManager{channels: map[string]chan<- MultipartMessage{}, session: session}
}

func (m ChannelSenderManager) AddChannel(channelId string) {
	if channelId != "" {
		if _, ok := m.channels[channelId]; !ok {
			m.channels[channelId] = createMessageSender(m.session, channelId)
		}
	}
}

func (m ChannelSenderManager) Get(channelId string) chan<- MultipartMessage {
	return m.channels[channelId]
}

func LogBeforeShutdown() {
	if err := recover(); err != nil {
		log.Println(err)
		os.Exit(1)
	}
}

// Remove "{{cmd}}" place holder and replace multiple space in row by one space
func CleanMessage(msg string) string {
	previousSpace := true
	newMsg := make([]rune, 0, len(msg))
	for _, char := range strings.ReplaceAll(msg, CmdPlaceHolder, "") {
		currentSpace := unicode.IsSpace(char)
		if currentSpace && previousSpace {
			continue
		}
		newMsg = append(newMsg, char)
		previousSpace = currentSpace
	}
	return string(newMsg)
}

func InitIdSet(names []string, nameToId map[string]string) StringSet {
	idSet := StringSet{}
	for _, name := range names {
		id := nameToId[name]
		if id == "" {
			panic("Unrecognized name (2) : " + name)
		}
		idSet[id] = Empty{}
	}
	return idSet
}

func IdInSet(ids []string, idSet StringSet) bool {
	for _, id := range ids {
		if _, ok := idSet[id]; ok {
			return true
		}
	}
	return false
}

func AppendCommand(cmds []*discordgo.ApplicationCommand, cmdData [2]string, options []*discordgo.ApplicationCommandOption) (string, []*discordgo.ApplicationCommand) {
	if cmdData[0] != "" {
		cmds = append(cmds, &discordgo.ApplicationCommand{
			Name: cmdData[0], Description: cmdData[1], Options: options,
		})
	}
	return cmdData[0], cmds
}

func AddNonEmpty[T any](m map[string]T, name string, value T) {
	if name != "" {
		m[name] = value
	}
}

func AuthorizedCmd(s *discordgo.Session, i *discordgo.InteractionCreate, infos GuildAndConfInfo, cmdEffect func() string) {
	returnMsg := infos.Msgs.ErrUnauthorized
	if IdInSet(i.Member.Roles, infos.AuthorizedRoleIds) {
		returnMsg = cmdEffect()
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: returnMsg},
	})
}

func MembersCmd(s *discordgo.Session, i *discordgo.InteractionCreate, messageSender chan<- MultipartMessage, infos GuildAndConfInfo, msgs Messages, userMonitor *IdMonitor, cmdEffect func(*discordgo.Member) int) {
	AuthorizedCmd(s, i, infos, func() string {
		go processMembers(s, messageSender, infos.GuildId, msgs, userMonitor, cmdEffect)
		return msgs.Ok
	})
}

func processMembers(s *discordgo.Session, messageSender chan<- MultipartMessage, guildId string, msgs Messages, userMonitor *IdMonitor, cmdEffect func(*discordgo.Member) int) {
	msg := msgs.EndedCmd
	if guildMembers, err := s.GuildMembers(guildId, "", MemberCallLimit); err == nil {
		if counterError := ProcessMembers(guildMembers, userMonitor, cmdEffect); counterError != 0 {
			msg = strings.ReplaceAll(msgs.ErrPartialCmd, NumErrorPlaceHolder, strconv.Itoa(counterError))
		}
	} else {
		log.Println("Cannot retrieve guild members (3) :", err)
		msg = msgs.ErrGlobalCmd
	}
	messageSender <- MultipartMessage{Message: msg}
}

func ProcessMembers(guildMembers []*discordgo.Member, userMonitor *IdMonitor, cmdEffect func(*discordgo.Member) int) int {
	counterError := 0
	for _, member := range guildMembers {
		if userId := member.User.ID; userMonitor.StartProcessing(userId) {
			counterError += cmdEffect(member)
			userMonitor.StopProcessing(userId)
		}
	}
	return counterError
}

func ExtractNick(member *discordgo.Member) (nick string) {
	if nick = member.Nick; nick != "" {
		return
	}
	user := member.User
	if nick = user.GlobalName; nick != "" {
		return
	}
	return user.Username
}

func createMessageSender(session *discordgo.Session, channelId string) chan<- MultipartMessage {
	messageChan := make(chan MultipartMessage)
	go sendMultiMessage(session, channelId, messageChan)
	return messageChan
}

func sendMultiMessage(session *discordgo.Session, channelId string, messageReceiver <-chan MultipartMessage) {
	for multiMessage := range messageReceiver {
		if message := strings.TrimSpace(multiMessage.Message); message == "" {
			if multiMessage.FileName != "" && multiMessage.FileData != "" {
				if sendFile(session, channelId, multiMessage.FileName, multiMessage.FileData) && multiMessage.ErrorMsg != "" {
					if _, err := session.ChannelMessageSend(channelId, multiMessage.ErrorMsg); err != nil {
						log.Println("Message sending failed (2) :", err)
					}
				}
			}
		} else {
			if multiMessage.FileName == "" || multiMessage.FileData == "" {
				if _, err := session.ChannelMessageSend(channelId, message); err != nil {
					log.Println("Message sending failed :", err)
				}
			} else {
				if multiMessage.AllowMerge && len(multiMessage.Message)+len(multiMessage.FileData) < 2000 {
					var builder strings.Builder
					builder.WriteString(message)
					builder.WriteByte('\n')
					builder.WriteString(multiMessage.FileData)
					if _, err := session.ChannelMessageSend(channelId, builder.String()); err != nil {
						log.Println("Message sending failed (3) :", err)
					}
				} else {
					dataReader := strings.NewReader(multiMessage.FileData)
					if _, err := session.ChannelFileSendWithMessage(channelId, message, multiMessage.FileName, dataReader); err != nil {
						log.Println("Message with file sending failed :", err)
					}
				}
			}
		}
	}
}

func sendFile(session *discordgo.Session, channelId string, path string, data string) bool {
	dataReader := strings.NewReader(data)
	if _, err := session.ChannelFileSend(channelId, path, dataReader); err != nil {
		log.Println("File sending failed :", err)
		return true
	}
	return false
}

func LaunchTickers(number int, interval time.Duration) []chan time.Time {
	subTickers := make([]chan time.Time, number)
	for index := range subTickers {
		subTickers[index] = make(chan time.Time, 1)
	}
	go startDispatchTick(interval, subTickers)
	return subTickers
}

func startDispatchTick(interval time.Duration, subTickers []chan time.Time) {
	dispatchTick(time.Now(), subTickers)
	for newTime := range time.Tick(interval) {
		dispatchTick(newTime, subTickers)
	}
}

func dispatchTick(newTime time.Time, subTickers []chan time.Time) {
	for _, subTicker := range subTickers {
		subTicker <- newTime
	}
}

func UpdateGameStatus(session *discordgo.Session, games []string, interval time.Duration) {
	if gamesLen := len(games); gamesLen != 0 {
		for range time.Tick(interval) {
			session.UpdateGameStatus(0, games[rand.Intn(gamesLen)])
		}
	}
}

func InitChecker(rule string) func(string) bool {
	if rule != "" {
		if colonIndex := strings.IndexByte(rule, ':'); colonIndex == -1 {
			log.Println("Check rule not recognized :", rule)
		} else {
			if re, err := regexp.Compile(rule[colonIndex+1:]); err == nil {
				if rule[:colonIndex] == "reject" {
					return func(link string) bool {
						return !re.MatchString(link)
					}
				}
				return re.MatchString
			} else {
				log.Println("Failed to compile regexp to check link :", err)
			}
		}
	}
	return acceptAll
}

func acceptAll(link string) bool {
	return true
}

func BuildMsgWithNameValueList(baseMsg string, nameToValue map[string]string) string {
	nameValues := make([][2]string, 0, len(nameToValue))
	for name, prefix := range nameToValue {
		nameValues = append(nameValues, [2]string{name, prefix})
	}
	slices.SortFunc(nameValues, cmpNameValueAsc)

	var buffer strings.Builder
	buffer.WriteString(baseMsg)
	for _, nameValue := range nameValues {
		buffer.WriteByte('\n')
		buffer.WriteString(nameValue[0])
		buffer.WriteString(" = ")
		buffer.WriteString(nameValue[1])
	}
	return buffer.String()
}

func cmpNameValueAsc(a [2]string, b [2]string) int {
	return cmp.Compare(a[0], b[0])
}

func SendTick(tickSender chan<- bool, interval time.Duration) {
	for range time.Tick(interval) {
		tickSender <- false
	}
}
