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

package gdrive

import (
	"context"
	"encoding/json"
	"log"
	"mime"
	"net/http"
	"os"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/dvaumoron/casiusbot/common"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

type DriveConfig struct {
	authConfig    *oauth2.Config
	tokenPath     string
	token         *oauth2.Token
	followLinkMsg string
	sendErrorMsg  string
	importFormats map[string][]string
}

func ReadDriveConfig(credentialsPath string, tokenPath string, followLinkMsg string, sendErrorMsg string) (DriveConfig, error) {
	credentialsData, err := os.ReadFile(credentialsPath)
	if err != nil {
		return DriveConfig{}, err
	}
	// If modifying these scopes, delete your previously saved token.json.
	config, err := google.ConfigFromJSON(credentialsData, drive.DriveScope)

	if err != nil {
		return DriveConfig{}, err
	}

	file, err := os.Open(tokenPath)
	if err != nil {
		return DriveConfig{}, err
	}
	defer file.Close()

	token := &oauth2.Token{}
	err = json.NewDecoder(file).Decode(token)
	if err != nil {
		return DriveConfig{}, err
	}

	driveConfig := DriveConfig{
		authConfig: config, tokenPath: tokenPath, token: token, followLinkMsg: followLinkMsg, sendErrorMsg: sendErrorMsg,
	}
	srv, err := driveConfig.newService()
	if err != nil {
		return DriveConfig{}, err
	}

	about, err := srv.About.Get().Fields("importFormats").Do()
	if err != nil {
		return DriveConfig{}, err
	}
	driveConfig.importFormats = about.ImportFormats
	return driveConfig, nil
}

func (config DriveConfig) newService() (*drive.Service, error) {
	ctx := context.Background()
	return drive.NewService(ctx, option.WithHTTPClient(config.authConfig.Client(ctx, config.token)))
}

func (config DriveConfig) CreateDriveSender(driveFolderId string, msgSender chan<- common.MultipartMessage) chan<- common.MultipartMessage {
	messageChan := make(chan common.MultipartMessage)
	go config.sendFileToDrive(driveFolderId, messageChan, msgSender)
	return messageChan
}

func (config DriveConfig) sendFileToDrive(driveFolderId string, dataReceiver <-chan common.MultipartMessage, msgSender chan<- common.MultipartMessage) {
	for multiMessage := range dataReceiver {
		srv, err := config.newService()
		if err != nil {
			log.Println("Unable to access Drive API :", err)
			continue
		}

		mimeType := ""
		if dotIndex := strings.LastIndexByte(multiMessage.FileName, '.'); dotIndex != -1 {
			mimeType = mime.TypeByExtension(multiMessage.FileName[dotIndex:])
		} else {
			mimeType = http.DetectContentType([]byte(multiMessage.FileData))
		}

		conversionMimeType := ""
		if formats := config.importFormats[cleanMimeType(mimeType)]; len(formats) != 0 {
			conversionMimeType = formats[0]
		}

		_, err = srv.Files.Create(
			&drive.File{
				Parents:  []string{driveFolderId},
				Name:     multiMessage.FileName,
				MimeType: conversionMimeType,
			},
		).Media(strings.NewReader(multiMessage.FileData), googleapi.ContentType(mimeType)).Do()
		if err != nil {
			if errMsg := err.Error(); strings.Contains(errMsg, "invalid_grant") {
				authURL := config.authConfig.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
				msgSender <- common.MultipartMessage{Message: strings.ReplaceAll(config.followLinkMsg, "{{link}}", authURL)}
			} else {
				log.Println("Unable to create file in Drive :", errMsg)
				msgSender <- common.MultipartMessage{Message: config.sendErrorMsg}
			}
		}
	}
}

func (config DriveConfig) DriveTokenCmd(s *discordgo.Session, i *discordgo.InteractionCreate, infos common.GuildAndConfInfo) {
	returnMsg := infos.Msgs[0]
	if common.IdInSet(i.Member.Roles, infos.AuthorizedRoleIds) {
		authCode := "TODO"

		if err := config.saveToken(authCode); err != nil {
			log.Println("Unable to save Google Drive token :", err)
			returnMsg = infos.Msgs[9]
		}
	} else {
		returnMsg = infos.Msgs[1]
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: returnMsg},
	})
}

// Write the token retrieved from browser in a file.
func (config DriveConfig) saveToken(authCode string) error {
	token, err := config.authConfig.Exchange(context.Background(), authCode)
	if err != nil {
		return err
	}

	file, err := os.OpenFile(config.tokenPath, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	return json.NewEncoder(file).Encode(token)
}

func cleanMimeType(mimetype string) string {
	if index := strings.IndexByte(mimetype, ';'); index != -1 {
		return mimetype[:index]
	}
	return mimetype
}
