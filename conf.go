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
	"errors"
	"io"
	"log"
	"os"
	"path"
	"strings"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
	"gopkg.in/yaml.v3"
)

type Config struct {
	basePath string
	data     map[string]any
}

func readConfig() (Config, error) {
	confPath := "casiusbot.yaml"
	if len(os.Args) > 1 {
		confPath = os.Args[1]
	}

	log.Println("Load configuration from", confPath)
	confBody, err := os.ReadFile(confPath)
	if err != nil {
		return Config{}, err
	}

	confData := map[string]any{}
	err = yaml.Unmarshal(confBody, confData)
	c := Config{basePath: path.Dir(confPath), data: confData}
	c.initLog("casiusbot.log")
	return c, err
}

func (c Config) updatePath(filePath string) string {
	if path.IsAbs(filePath) {
		// no change for absolute path
		return filePath
	}
	return path.Join(c.basePath, filePath)
}

func (c Config) initLog(logPath string) {
	log.SetOutput(io.MultiWriter(log.Writer(), &lumberjack.Logger{
		Filename:   c.updatePath(logPath),
		MaxSize:    1, // megabytes
		MaxBackups: 5,
		MaxAge:     28, //days
	}))
}

func (c Config) getString(valueConfName string) string {
	value, _ := c.data[valueConfName].(string)
	return value
}

func (c Config) require(valueConfName string) string {
	value := c.getString(valueConfName)
	if value == "" {
		log.Fatalln("Configuration value is missing :", valueConfName)
	}
	return value
}

func (c Config) readPrefixConfig(filePathName string) (map[string]string, []string, [][2]string, []string) {
	// TODO merge the prefix config in the yaml file
	file, err := os.Open(c.updatePath(c.getString(filePathName)))
	if err != nil {
		log.Fatalln("Cannot read the configuration file :", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	nameToPrefix := map[string]string{}
	prefixes := []string{}
	cmdAndNames := [][2]string{}
	specialRoles := []string{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && line[0] != '#' {
			splitted := strings.Split(line, ":")
			if splittedSize := len(splitted); splittedSize > 1 {
				name := strings.TrimSpace(splitted[0])
				if name != "" {
					prefix := strings.TrimSpace(splitted[1]) + " "

					nameToPrefix[name] = prefix
					prefixes = append(prefixes, prefix)

					if splittedSize > 2 {
						cmd := strings.TrimSpace(splitted[2])
						if cmd == "" {
							log.Fatalln("Malformed configuration file, empty command")
						}
						cmdAndNames = append(cmdAndNames, [2]string{cmd, name})
					} else {
						specialRoles = append(specialRoles, name)
					}
				}
			}
		}
	}

	if err = scanner.Err(); err != nil {
		log.Fatalln("Cannot parse the configuration file :", err)
	}
	return nameToPrefix, prefixes, cmdAndNames, specialRoles
}

func (c Config) getIdSet(namesConfName string, nameToId map[string]string) (map[string]empty, error) {
	names, ok := c.data[namesConfName].([]any)
	if !ok {
		return nil, nil
	}
	idSet := map[string]empty{}
	for _, name := range names {
		nameStr, ok := name.(string)
		if !ok {
			return nil, errors.New("a value is not a string in " + namesConfName)
		}
		id := nameToId[nameStr]
		if id == "" {
			return nil, errors.New("Unrecognized name : " + nameStr)
		}
		idSet[id] = empty{}
	}
	return idSet, nil
}

func (c Config) getSlice(valuesConfName string) []any {
	values, _ := c.data[valuesConfName].([]any)
	return values
}

func (c Config) getStringSlice(valuesConfName string) []string {
	values, ok := c.data[valuesConfName].([]any)
	if !ok {
		return nil
	}
	casted := make([]string, 0, len(values))
	for _, value := range values {
		valueStr, ok := value.(string)
		if !ok {
			log.Fatalln("a value is not a string in", valuesConfName)
		}
		casted = append(casted, valueStr)
	}
	return casted
}

func (c Config) getDurationSec(valueConfName string) time.Duration {
	value := c.data[valueConfName]
	valueSec, ok := value.(int)
	if !ok {
		log.Printf("Configuration %v is not an integer (%T)", valueConfName, value)
	}
	return time.Duration(valueSec) * time.Second
}

func (c Config) getDelayMins(valuesConfName string) []time.Duration {
	values, _ := c.data[valuesConfName].([]any)
	delays := make([]time.Duration, 0, len(values))
	for _, value := range values {
		valueMin, ok := value.(int)
		if !ok {
			log.Fatalf("Configuration %v is not an integer (%T)", valuesConfName, value)
		}
		delay := time.Duration(valueMin) * time.Minute
		if delay > 0 {
			delay = -delay
		}
		delays = append(delays, delay)
	}
	return delays
}
