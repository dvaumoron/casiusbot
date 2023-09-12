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
	"fmt"
	"log"
	"mime"
	"net/http"
	"os"
	"strings"

	"github.com/dvaumoron/casiusbot/common"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

type DriveConfig struct {
	authConfig    *oauth2.Config
	token         *oauth2.Token
	importFormats map[string][]string
}

func ReadDriveConfig(credentialsPath string, tokenPath string) (DriveConfig, error) {
	config, err := readOAuthConfig(credentialsPath)
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

	driveConfig := DriveConfig{authConfig: config, token: token}
	srv, err := driveConfig.NewService()
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

func (config DriveConfig) NewService() (*drive.Service, error) {
	ctx := context.Background()
	return drive.NewService(ctx, option.WithHTTPClient(config.authConfig.Client(ctx, config.token)))
}

func readOAuthConfig(credentialsPath string) (*oauth2.Config, error) {
	credentialsData, err := os.ReadFile(credentialsPath)
	if err != nil {
		return nil, err
	}
	// If modifying these scopes, delete your previously saved token.json.
	return google.ConfigFromJSON(credentialsData, drive.DriveScope)
}

// Request a token from the web, then write the retrieved token in a file.
func SaveTokenFromWeb(ctx context.Context, credentialsPath string, tokenPath string) {
	config, err := readOAuthConfig(credentialsPath)
	if err != nil {
		log.Println("Unable to read OAuth config :", err)
		return
	}

	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Println("Go to the following link in your browser then type the authorization code :", authURL)

	var authCode string
	if _, err := fmt.Scan(&authCode); err != nil {
		log.Println("Unable to read authorization code :", err)
	}

	token, err := config.Exchange(ctx, authCode)
	if err != nil {
		log.Println("Unable to retrieve token from web :", err)
	}

	log.Println("Saving credential file to :", tokenPath)
	file, err := os.OpenFile(tokenPath, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		log.Println("Unable to cache oauth token :", err)
		return
	}
	defer file.Close()

	json.NewEncoder(file).Encode(token)
}

func CreateDriveSender(config DriveConfig, driveFolderId string) chan<- common.MultipartMessage {
	messageChan := make(chan common.MultipartMessage)
	go sendFileToDrive(config, driveFolderId, messageChan)
	return messageChan
}

func sendFileToDrive(config DriveConfig, driveFolderId string, dataReceiver <-chan common.MultipartMessage) {
	for multiMessage := range dataReceiver {
		srv, err := config.NewService()
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
		if formats := config.importFormats[mimeType]; len(formats) != 0 {
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
			log.Println("Unable to create file in Drive :", err)
		}
	}
}
