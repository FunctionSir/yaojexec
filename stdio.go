/*
 * @Author: FunctionSir
 * @License: AGPLv3
 * @Date: 2025-05-08 21:03:16
 * @LastEditTime: 2025-05-08 21:54:25
 * @LastEditors: FunctionSir
 * @Description: -
 * @FilePath: /yaojexec/stdio.go
 */
package main

import (
	"io"
	"os"
)

func getStdin(x *Task) (io.Reader, error) {
	if x.Stdin == "stdin" {
		return os.Stdin, nil
	}
	r, err := os.Open(x.Stdin)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func getStdout(x *Task) (io.Writer, error) {
	if x.Stdout == "stdout" {
		return os.Stdout, nil
	}
	w, err := os.OpenFile(x.Stdout, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	return w, nil
}

func getStderr(x *Task) (io.Writer, error) {
	if x.Stderr == "stderr" {
		return os.Stderr, nil
	}
	w, err := os.OpenFile(x.Stderr, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	return w, nil
}
