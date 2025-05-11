/*
 * @Author: FunctionSir
 * @License: AGPLv3
 * @Date: 2025-05-07 20:46:09
 * @LastEditTime: 2025-05-11 20:49:13
 * @LastEditors: FunctionSir
 * @Description: -
 * @FilePath: /yaojexec/main.go
 */

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/process"
)

// Pre-Defined Status
const (
	STATUS_OK  = "OK"  // OK
	STATUS_RE  = "RE"  // Runtime Error
	STATUS_MLE = "MLE" // Memory Limit Exceeded
	STATUS_TLE = "TLE" // Time Limit Exceeded
	STATUS_OLE = "OLE" // Output Limit Exceeded
	STATUS_IE  = "IE"  // Internal Error
)

func getNobody() (uint32, uint32, error) {
	nobody, err := user.Lookup("nobody")
	if err != nil {
		panic(err)
	}
	nobodyUid, err := strconv.Atoi(nobody.Uid)
	if err != nil {
		return 65535, 65535, err
	}
	nobodyGid, err := strconv.Atoi(nobody.Gid)
	if err != nil {
		return 65535, 65535, err
	}
	return uint32(nobodyUid), uint32(nobodyGid), nil
}

func chkAndHandleInternalError(err error, entry *LogEntry, logContent *Logs) bool {
	if err != nil {
		entry.Status = STATUS_IE
		entry.MemUsed = uint64(0)
		entry.TimeUsed = float64(0)
		logContent.Logs = append(logContent.Logs, *entry)
	}
	return err != nil
}

func main() {
	if len(os.Args) <= 1 {
		panic("no enough args")
	}
	conf, err := NewConfig(os.Args[1])
	if err != nil {
		panic(err)
	}
	nobodyUid, nobodyGid, err := getNobody()
	if err != nil {
		panic(err)
	}
	name := conf.Name
	args := conf.Args
	logContent := Logs{Logs: make([]LogEntry, 0)}
	var finished atomic.Bool
	var c *exec.Cmd
	var p *process.Process
	var cpuTime *cpu.TimesStat
	var mem *process.MemoryInfoStat
	var maxRSS uint64
	var maxCpuUserTime float64
	var stdoutStat os.FileInfo
	var stderrStat os.FileInfo
	logContent.Total = len(conf.Tasks)
	logContent.Ok = 0
	startedAt := time.Now()
	for _, x := range conf.Tasks {
		entry := LogEntry{Status: ""}
		c = exec.Command(name, args...)
		c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		c.SysProcAttr.Credential = &syscall.Credential{Uid: nobodyUid, Gid: nobodyGid}
		// Stdin //
		c.Stdin, err = getStdin(&x)
		if chkAndHandleInternalError(err, &entry, &logContent) {
			if conf.ContinueOnErr {
				continue
			}
			break
		}
		// Stdout //
		c.Stdout, err = getStdout(&x)
		if chkAndHandleInternalError(err, &entry, &logContent) {
			if conf.ContinueOnErr {
				continue
			}
			break
		}
		// Stderr //
		if x.Stdout == x.Stderr {
			c.Stderr = c.Stdout
		} else {
			c.Stderr, err = getStderr(&x)
			if chkAndHandleInternalError(err, &entry, &logContent) {
				if conf.ContinueOnErr {
					continue
				}
				break
			}
		}
		maxRSS = uint64(0)
		maxCpuUserTime = float64(0)
		err = c.Start()
		if chkAndHandleInternalError(err, &entry, &logContent) {
			if conf.ContinueOnErr {
				continue
			}
			break
		}
		p, err = process.NewProcess(int32(c.Process.Pid))
		if chkAndHandleInternalError(err, &entry, &logContent) {
			if conf.ContinueOnErr {
				continue
			}
			break
		}
		finished.Store(false)
		c.Start()
		// Status Logger //
		go func() {
			for {
				mem, err = p.MemoryInfo()
				if err != nil {
					if finished.Load() {
						break
					}
					continue
				}
				cpuTime, err = p.Times()
				if err != nil {
					if finished.Load() {
						break
					}
					continue
				}
				maxCpuUserTime = max(maxCpuUserTime, cpuTime.User)
				maxRSS = max(maxRSS, mem.RSS)
				if x.MemLimit != 0 && maxRSS > x.MemLimit {
					entry.Status = STATUS_MLE
					err = syscall.Kill(c.Process.Pid, syscall.SIGKILL)
					if err != nil {
						panic(err)
					}
					break
				}
				if x.TimeLimit != 0 && maxCpuUserTime > float64(x.TimeLimit) {
					entry.Status = STATUS_TLE
					err = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
					if err != nil {
						panic(err)
					}
					break
				}
				if x.StdoutLimit != 0 && x.Stdout != "stdout" {
					stdoutStat, err = os.Stat(x.Stdout)
					if err == nil && stdoutStat.Size() > x.StdoutLimit {
						entry.Status = STATUS_OLE
						err = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
						if err != nil {
							panic(err)
						}
						break
					}
				}
				if x.StderrLimit != 0 && x.Stderr != "stderr" {
					stderrStat, err = os.Stat(x.Stderr)
					if err == nil && stderrStat.Size() > x.StderrLimit {
						entry.Status = STATUS_OLE
						err = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
						if err != nil {
							panic(err)
						}
						break
					}
				}
				if finished.Load() {
					break
				}
			}
		}()
		err = c.Wait() // Wait Until Done.
		finished.Store(true)
		if entry.Status == "" {
			if err != nil {
				entry.Status = STATUS_RE
			} else {
				entry.Status = STATUS_OK
				logContent.Ok++
			}
		}
		entry.MemUsed = maxRSS
		entry.TimeUsed = maxCpuUserTime
		entry.StdoutSize = 0
		if x.Stdout != "stdout" {
			stdoutStat, err = os.Stat(x.Stdout)
			if err == nil {
				entry.StdoutSize = stdoutStat.Size()
			}
		}
		entry.StderrSize = 0
		if x.Stderr != "stderr" {
			stderrStat, err = os.Stat(x.Stderr)
			if err == nil {
				entry.StderrSize = stderrStat.Size()
			}
		}
		logContent.Logs = append(logContent.Logs, entry)
		if err != nil && !conf.ContinueOnErr {
			break
		}
	}
	logContent.TotalTime = time.Since(startedAt).Seconds()
	logContent.NotStarted = logContent.Total - len(logContent.Logs)
	logContent.Failed = logContent.Total - logContent.Ok - logContent.NotStarted
	logBytes, err := json.Marshal(logContent)
	if err != nil {
		panic(err)
	}
	err = os.WriteFile(conf.LogFile, logBytes, 0600)
	if err != nil {
		panic(err)
	}
}
