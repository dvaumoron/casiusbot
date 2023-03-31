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
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
)

func main() {
	if godotenv.Overload() == nil {
		fmt.Println("Loaded .env file")
	}

	session, err := discordgo.New("Bot " + os.Getenv("BOT_TOKEN"))
	if err != nil {
		fmt.Println("An error occured :", err)
	}

	okCmdMsg := os.Getenv("MESSAGE_CMD_OK")
	errPartialCmdMsg := os.Getenv("MESSAGE_CMD_PARTIAL_ERROR")
	errGlobalCmdMsg := os.Getenv("MESSAGE_CMD_GLOBAL_ERROR")

	session.Identify.Intents |= discordgo.IntentGuildMembers

	applyCmd := &discordgo.ApplicationCommand{
		Name:        "apply-prefix",
		Description: "Apply the prefix rule to all User",
	}
	cleanCmd := &discordgo.ApplicationCommand{
		Name:        "clean-prefix",
		Description: "Clean the prefix for all User",
	}

	filePath := os.Getenv("PREFIX_FILE_PATH")
	nameToPrefix, err := readPrefixConfig(filePath)
	if err != nil {
		log.Fatalln("Cannot open the configuration file :", err)
	}

	prefixes := make([]string, 0, len(nameToPrefix))
	for _, prefix := range nameToPrefix {
		prefixes = append(prefixes, prefix)
	}

	guildId := os.Getenv("GUILD_ID")

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

	guildRoles, err := session.GuildRoles(guildId)
	if err != nil {
		log.Println("Cannot retrieve roles of the guild :", err)
		return
	}

	roleIdToPrefix := map[string]string{}
	for _, guildRole := range guildRoles {
		if prefix, ok := nameToPrefix[guildRole.Name]; ok {
			roleIdToPrefix[guildRole.ID] = prefix
		}
	}

	var mutex sync.RWMutex
	cmdworking := false

	session.AddHandler(func(s *discordgo.Session, u *discordgo.GuildMemberUpdate) {
		if userId := u.User.ID; userId != ownerId {
			mutex.RLock()
			cmdworking2 := cmdworking
			mutex.RUnlock()

			if !cmdworking2 {
				nickName := u.Member.Nick
				if nickName == "" {
					nickName = u.User.Username
				}

				newNickName := transformName(nickName, u.Roles, roleIdToPrefix, prefixes)
				if newNickName != nickName {
					if err = s.GuildMemberNickname(u.GuildID, userId, newNickName); err != nil {
						log.Println("An error occurred (1) :", err)
					}
				}
			}
		}
	})

	session.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		switch i.ApplicationCommandData().Name {
		case applyCmd.Name:
			mutex.Lock()
			cmdworking = true
			mutex.Unlock()

			guildMembers, err := s.GuildMembers(i.GuildID, "", 1000)
			returnMsg := okCmdMsg
			if err == nil {
				counterError := 0
				for _, guildMember := range guildMembers {
					if userId := guildMember.User.ID; userId != ownerId {
						nickName := guildMember.Nick
						if nickName == "" {
							nickName = guildMember.User.Username
						}

						newNickName := transformName(nickName, guildMember.Roles, roleIdToPrefix, prefixes)
						if newNickName != nickName {
							if err = s.GuildMemberNickname(i.GuildID, guildMember.User.ID, newNickName); err != nil {
								log.Println("An error occurred (2) :", err)
								counterError++
							}
						}
					}
				}

				if counterError != 0 {
					returnMsg = buildPartialErrorString(errPartialCmdMsg, counterError)
				}
			} else {
				log.Println("An error occurred (3) :", err)
				returnMsg = errGlobalCmdMsg
			}

			mutex.Lock()
			cmdworking = false
			mutex.Unlock()

			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{Content: returnMsg},
			})
		case cleanCmd.Name:
			mutex.Lock()
			cmdworking = true
			mutex.Unlock()

			guildMembers, err := s.GuildMembers(i.GuildID, "", 1000)
			returnMsg := okCmdMsg
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
								log.Println("An error occurred (4) :", err)
								counterError++
							}
						}
					}
				}

				if counterError != 0 {
					returnMsg = buildPartialErrorString(errPartialCmdMsg, counterError)
				}
			} else {
				log.Println("An error occurred (5) :", err)
				returnMsg = errGlobalCmdMsg
			}

			mutex.Lock()
			cmdworking = false
			mutex.Unlock()

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

func readPrefixConfig(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	nameToPrefix := map[string]string{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) != 0 && line[0] != '#' {
			sepIndex := strings.IndexByte(line, '=')
			nameToPrefix[strings.TrimSpace(line[:sepIndex])] = strings.TrimSpace(line[sepIndex+1:]) + " "
		}
	}
	return nameToPrefix, nil
}

func transformName(nickName string, roleIds []string, roleIdToPrefix map[string]string, prefixes []string) string {
	nickName = cleanPrefix(nickName, prefixes)
	for _, roleId := range roleIds {
		if prefix, ok := roleIdToPrefix[roleId]; ok {
			// prefix already end with a space
			return prefix + nickName
		}
	}
	return nickName
}

func cleanPrefix(nickName string, prefixes []string) string {
	for _, prefix := range prefixes {
		if strings.HasPrefix(nickName, prefix) {
			return nickName[len(prefix):]
		}
	}
	return nickName
}

func buildPartialErrorString(s string, i int) string {
	var buffer strings.Builder
	buffer.WriteString(s)
	buffer.WriteByte(' ')
	buffer.WriteString(strconv.Itoa(i))
	return buffer.String()
}
