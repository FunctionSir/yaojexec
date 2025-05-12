/*
 * @Author: FunctionSir
 * @License: AGPLv3
 * @Date: 2025-05-08 21:04:29
 * @LastEditTime: 2025-05-12 22:05:03
 * @LastEditors: FunctionSir
 * @Description: -
 * @FilePath: /yaojexec/types.go
 */

package main

import (
	"encoding/json"
	"os"
)

type Task struct {
	Stdin         string  `json:"stdin"`
	Stdout        string  `json:"stdout"`
	Stderr        string  `json:"stderr"`
	MemLimit      uint64  `json:"mem-limit"`
	CpuTimeLimit  float64 `json:"cpu-time-limit"`
	RealTimeLimit float64 `json:"real-time-limit"`
	StdoutLimit   int64   `json:"stdout-limit"`
	StderrLimit   int64   `json:"stderr-limit"`
}

type Config struct {
	Name          string   `json:"name"`
	Args          []string `json:"args"`
	ContinueOnErr bool     `json:"continue-on-err"`
	Tasks         []Task   `json:"tasks"`
	LogFile       string   `json:"log-file"`
}

type LogEntry struct {
	Status       string  `json:"status"`
	MemUsed      uint64  `json:"mem-used"`
	CpuTimeUsed  float64 `json:"cpu-time-used"`
	RealTimeUsed float64 `json:"real-time-used"`
	StdoutSize   int64   `json:"stdout-size"`
	StderrSize   int64   `json:"stderr-size"`
}

type Logs struct {
	Total      int        `json:"total"`
	Ok         int        `json:"ok"`
	Failed     int        `json:"failed"`
	NotStarted int        `json:"not-started"`
	Logs       []LogEntry `json:"logs"`
	TotalTime  float64    `json:"total-time"`
}

func NewConfig(path string) (Config, error) {
	var conf Config
	file, err := os.ReadFile(path)
	if err != nil {
		return conf, err
	}
	err = json.Unmarshal(file, &conf)
	if err != nil {
		return conf, err
	}
	return conf, nil
}
