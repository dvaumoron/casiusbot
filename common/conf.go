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
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
	"gopkg.in/yaml.v3"
)

const (
	notStringMsg  = "Configuration %v contains a non string : %v (%T)"
	notIntegerMsg = "Configuration %v contains a non integer : %v (%T)"
)

type Config struct {
	basePath string
	data     map[string]any
}

func ReadConfig() Config {
	confPath := "casiusbot.yaml"
	if len(os.Args) > 1 {
		confPath = os.Args[1]
	}

	log.Println("Load configuration from", confPath)
	confBody, err := os.ReadFile(confPath)
	if err != nil {
		panic(fmt.Sprint("Unable to read configuration :", err))
	}

	confData := map[string]any{}
	if err = yaml.Unmarshal(confBody, confData); err != nil {
		panic(fmt.Sprint("Unable to parse configuration :", err))
	}

	c := Config{basePath: path.Dir(confPath), data: confData}
	c.initLog()
	return c
}

func (c Config) updatePath(filePath string) string {
	if path.IsAbs(filePath) {
		// no change for absolute path
		return filePath
	}
	return path.Join(c.basePath, filePath)
}

func (c Config) initLog() {
	logPath := c.GetString("LOG_PATH")
	if logPath == "" {
		logPath = "casiusbot.log"
	}
	log.SetOutput(io.MultiWriter(&lumberjack.Logger{
		Filename:   c.updatePath(logPath),
		MaxSize:    1, // megabytes
		MaxBackups: 5,
		MaxAge:     28, //days
	}, os.Stderr))
}

func (c Config) GetString(valueConfName string) string {
	value, _ := c.data[valueConfName].(string)
	return value
}

func (c Config) Require(valueConfName string) string {
	value := c.GetString(valueConfName)
	if value == "" {
		panic("Configuration value is missing : " + valueConfName)
	}
	return value
}

func (c Config) GetPrefixConfig() (map[string]string, []string, [][2]string, []string) {
	rules, ok := c.data["PREFIX_RULES"].([]any)
	if !ok {
		panic("Malformed PREFIX_RULES")
	}

	nameToPrefix := map[string]string{}
	prefixes := []string{}
	cmdAndNames := [][2]string{}
	specialRoles := []string{}
	for _, rule := range rules {
		casted, ok := rule.(map[string]any)
		if !ok {
			panic("Malformed rule")
		}

		if name, _ := casted["ROLE"].(string); name != "" {
			prefix, _ := casted["PREFIX"].(string)
			if prefix == "" {
				panic("Rule without PREFIX : " + name)
			}

			nameToPrefix[name] = prefix + " "
			prefixes = append(prefixes, prefix)

			if cmd, _ := casted["CMD"].(string); cmd == "" {
				specialRoles = append(specialRoles, name)
			} else {
				cmdAndNames = append(cmdAndNames, [2]string{cmd, name})
			}
		}
	}
	return nameToPrefix, prefixes, cmdAndNames, specialRoles
}

func (c Config) GetCommandConfig() map[string][2]string {
	cmds, ok := c.data["CMDS"].(map[string]any)
	if !ok {
		panic("Malformed CMDS")
	}

	res := map[string][2]string{}
	for cmdName, cmdData := range cmds {
		casted, ok := cmdData.(map[string]any)
		if !ok {
			panic("Malformed command : " + cmdName)
		}

		if cmd, _ := casted["CMD"].(string); cmd != "" {
			desc, _ := casted["DESCRIPTION"].(string)
			if desc == "" {
				panic("Command without DESCRIPTION : " + cmdName)
			}
			res[cmdName] = [2]string{cmd, desc}
		}
	}
	return res
}

func (c Config) GetIdSet(namesConfName string, nameToId map[string]string) StringSet {
	names, ok := c.data[namesConfName].([]any)
	if !ok {
		return nil
	}
	idSet := StringSet{}
	for _, name := range names {
		nameStr, ok := name.(string)
		if !ok {
			panic(fmt.Sprintf(notStringMsg, namesConfName, name, name))
		}
		id := nameToId[nameStr]
		if id == "" {
			panic("Unrecognized name : " + nameStr)
		}
		idSet[id] = Empty{}
	}
	return idSet
}

func (c Config) GetSlice(valuesConfName string) []any {
	values, _ := c.data[valuesConfName].([]any)
	return values
}

func (c Config) GetStringSlice(valuesConfName string) []string {
	values, ok := c.data[valuesConfName].([]any)
	if !ok {
		return nil
	}
	casted := make([]string, 0, len(values))
	for _, value := range values {
		valueStr, ok := value.(string)
		if !ok {
			panic(fmt.Sprintf(notStringMsg, valuesConfName, value, value))
		}
		casted = append(casted, valueStr)
	}
	return casted
}

func (c Config) GetDurationSec(valueConfName string) time.Duration {
	value := c.data[valueConfName]
	valueSec, ok := value.(int)
	if !ok {
		log.Printf(notIntegerMsg, valueConfName, value, value)
	}
	return time.Duration(valueSec) * time.Second
}

func (c Config) GetDelayMins(valuesConfName string) []time.Duration {
	values, _ := c.data[valuesConfName].([]any)
	delays := make([]time.Duration, 0, len(values))
	for _, value := range values {
		valueMin, ok := value.(int)
		if !ok {
			panic(fmt.Sprintf(notIntegerMsg, valuesConfName, value, value))
		}
		delay := time.Duration(valueMin) * time.Minute
		if delay > 0 {
			delay = -delay
		}
		delays = append(delays, delay)
	}
	return delays
}

func (c Config) GetPath(pathConfName string) string {
	path := c.GetString(pathConfName)
	if path != "" {
		path = c.updatePath(path)
	}
	return path
}
