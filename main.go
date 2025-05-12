/*
 * @Author: FunctionSir
 * @License: AGPLv3
 * @Date: 2025-05-07 20:46:09
 * @LastEditTime: 2025-05-12 22:45:42
 * @LastEditors: FunctionSir
 * @Description: -
 * @FilePath: /yaojexec/main.go
 */

package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"os/user"
	"strconv"
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

// Get UID and GID of user nobody
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

// Check and handle internal errors
func chkAndHandleInternalError(err error, entry *LogEntry, logContent *Logs) bool {
	if err != nil {
		entry.Status = STATUS_IE
		entry.MemUsed = uint64(0)
		entry.CpuTimeUsed = float64(0)
		logContent.Logs = append(logContent.Logs, *entry)
	}
	return err != nil
}

func main() {
	if len(os.Args) <= 1 {
		panic("no enough args")
	}

	// Read config //
	conf, err := NewConfig(os.Args[1])
	if err != nil {
		panic(err)
	}

	// Get UID and GID of user nobody //
	nobodyUid, nobodyGid, err := getNobody()
	if err != nil {
		panic(err)
	}

	// Get name and args to run //
	name := conf.Name
	args := conf.Args

	// Init log content //
	logContent := Logs{Logs: make([]LogEntry, 0)}
	logContent.Total = len(conf.Tasks)
	logContent.Ok = 0

	// Important vars //
	var c *exec.Cmd                 // Command
	var p *process.Process          // Process info
	var cpuTime *cpu.TimesStat      // CPU time info
	var mem *process.MemoryInfoStat // Mem usage info
	var maxRSS uint64               // Max RSS used
	var maxCpuUserTime float64      // Max User CPU time used
	var stdoutStat os.FileInfo      // Stat of stdout
	var stderrStat os.FileInfo      // Stat of stderr
	var stdin io.Reader             // Stdin
	var stdout io.Writer            // Stdout
	var stderr io.Writer            // Stderr
	var ctx context.Context         // Cmd context
	var cancel context.CancelFunc   // Cancel func of ctx
	var loggerDone chan struct{}    // Chan to notify logger done
	var ctxCreatedAt time.Time      // ctx Created Time

	// Save start time of hole job //
	totalStartedAt := time.Now()

	// For each task //
	for _, x := range conf.Tasks {
		// New log entry //
		entry := LogEntry{Status: ""}

		// Get stdin //
		stdin, err = getStdin(&x)
		if chkAndHandleInternalError(err, &entry, &logContent) {
			if conf.ContinueOnErr {
				continue
			}
			break
		}

		// Get stdout //
		stdout, err = getStdout(&x)
		if chkAndHandleInternalError(err, &entry, &logContent) {
			if conf.ContinueOnErr {
				continue
			}
			break
		}

		// Get stderr //
		if x.Stdout == x.Stderr {
			stderr = stdout
		} else {
			stderr, err = getStderr(&x)
			if chkAndHandleInternalError(err, &entry, &logContent) {
				if conf.ContinueOnErr {
					continue
				}
				break
			}
		}

		// Init maxRSS and maxCpuUserTime //
		maxRSS = uint64(0)
		maxCpuUserTime = float64(0)

		// Make loggerDone chan
		loggerDone = make(chan struct{}, 1)
		defer close(loggerDone) // To avoid resources leakage

		// Create ctx //
		if x.RealTimeLimit != 0 {
			ctx, cancel = context.WithTimeout(context.Background(), time.Duration(x.RealTimeLimit*float64(time.Second)))
		} else {
			ctx, cancel = context.WithCancel(context.Background())
		}
		ctxCreatedAt = time.Now()
		defer cancel() // To avoid resources leakage

		// Create cmd //
		c = exec.CommandContext(ctx, name, args...)

		// Run as nobody //
		c.SysProcAttr = &syscall.SysProcAttr{}
		c.SysProcAttr.Credential = &syscall.Credential{Uid: nobodyUid, Gid: nobodyGid}

		// Set stdin, stdout, stderr //
		c.Stdin = stdin
		c.Stdout = stdout
		c.Stderr = stderr

		// Start the program
		err = c.Start()
		if chkAndHandleInternalError(err, &entry, &logContent) {
			if conf.ContinueOnErr {
				continue
			}
			break
		}

		// Get process info //
		p, err = process.NewProcess(int32(c.Process.Pid))
		if chkAndHandleInternalError(err, &entry, &logContent) {
			if conf.ContinueOnErr {
				continue
			}
			break
		}

		// Start the program //
		c.Start()

		// Status Logger //
		go func() {
			for {
				// Get mem info //
				mem, err = p.MemoryInfo()
				if err != nil {
					if c.ProcessState != nil {
						loggerDone <- struct{}{}
						break
					}
					continue
				}

				// Get CPU time info //
				cpuTime, err = p.Times()
				if err != nil {
					if c.ProcessState != nil {
						loggerDone <- struct{}{}
						break
					}
					continue
				}

				// Update max usage //
				maxCpuUserTime = max(maxCpuUserTime, cpuTime.User)
				maxRSS = max(maxRSS, mem.RSS)

				// MLE //
				if x.MemLimit != 0 && maxRSS > x.MemLimit {
					entry.Status = STATUS_MLE
					cancel()
					loggerDone <- struct{}{}
					break
				}

				// TLE //
				if x.CpuTimeLimit != 0 && maxCpuUserTime > float64(x.CpuTimeLimit) {
					entry.Status = STATUS_TLE
					cancel()
					loggerDone <- struct{}{}
					break
				}

				// OLE (stdout) //
				if x.StdoutLimit != 0 && x.Stdout != "stdout" {
					stdoutStat, err = os.Stat(x.Stdout)
					if err == nil && stdoutStat.Size() > x.StdoutLimit {
						entry.Status = STATUS_OLE
						cancel()
						loggerDone <- struct{}{}
						break
					}
				}

				// OLE (stderr) //
				if x.StderrLimit != 0 && x.Stderr != "stderr" {
					stderrStat, err = os.Stat(x.Stderr)
					if err == nil && stderrStat.Size() > x.StderrLimit {
						entry.Status = STATUS_OLE
						cancel()
						loggerDone <- struct{}{}
						break
					}
				}

				// Done //
				if c.ProcessState != nil {
					loggerDone <- struct{}{}
					break
				}
			}
		}()

		// Wait until program done //
		err = c.Wait()

		// Real time used //
		entry.RealTimeUsed = time.Since(ctxCreatedAt).Seconds()

		// Wait until logger done //
		<-loggerDone

		// If status is empty //
		if entry.Status == "" {
			if ctx.Err() == context.DeadlineExceeded { // Real time exceeded
				entry.Status = STATUS_TLE
			} else {
				if err != nil { // Runtime error
					entry.Status = STATUS_RE
				} else { // No errors
					entry.Status = STATUS_OK
					logContent.Ok++
				}
			}
		}

		// Fill other fields //
		entry.MemUsed = maxRSS
		entry.CpuTimeUsed = maxCpuUserTime
		entry.StdoutSize = 0 // If use stdout as stdout, no statics
		if x.Stdout != "stdout" {
			stdoutStat, err = os.Stat(x.Stdout)
			if err == nil {
				entry.StdoutSize = stdoutStat.Size()
			}
		}
		entry.StderrSize = 0 // If use stderr as stderr, no statics
		if x.Stderr != "stderr" {
			stderrStat, err = os.Stat(x.Stderr)
			if err == nil {
				entry.StderrSize = stderrStat.Size()
			}
		}

		// Append the log entry //
		logContent.Logs = append(logContent.Logs, entry)

		// If error occured, continue or not //
		if err != nil && !conf.ContinueOnErr {
			break
		}
	}

	// Fill other log fields //
	logContent.TotalTime = time.Since(totalStartedAt).Seconds()
	logContent.NotStarted = logContent.Total - len(logContent.Logs)
	logContent.Failed = logContent.Total - logContent.Ok - logContent.NotStarted

	// JSON marshal //
	logBytes, err := json.Marshal(logContent)
	if err != nil {
		panic(err)
	}

	// Write to log file //
	err = os.WriteFile(conf.LogFile, logBytes, 0600)
	if err != nil {
		panic(err)
	}
}
