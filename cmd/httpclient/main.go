package main

import (
	"github.com/gobs/args"
	"github.com/gobs/cmd"
	"github.com/gobs/httpclient"
	"github.com/gobs/simplejson"

	"bytes"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	completion_words = []string{}
)

func CompletionFunction(text, line string) (matches []string) {
	// for the "ls" command we let readline show real file names
	if strings.HasPrefix(line, "ls ") {
		return
	}

	// for all other commands, we pick from our list of completion words
	for _, w := range completion_words {
		if strings.HasPrefix(w, text) {
			matches = append(matches, w)
		}
	}

	return
}

func main() {
	var interrupted bool
	var client = httpclient.NewHttpClient("")

	commander := &cmd.Cmd{
		HistoryFile: ".httpclient_history",
		Complete:    CompletionFunction,
		EnableShell: true,
		Interrupt:   func(sig os.Signal) bool { interrupted = true; return false },
	}

	commander.Init()

	commander.Vars = map[string]string{}

	commander.Add(cmd.Command{
		"base",
		`base [url]`,
		func(line string) (stop bool) {
			if line != "" {
				val, err := url.Parse(line)
				if err != nil {
					fmt.Println(err)
					return
				}

				client.BaseURL = val
				commander.Prompt = fmt.Sprintf("%v> ", client.BaseURL)
			}

			return
		},
		nil})

	commander.Add(cmd.Command{
		"insecure",
		`insecure [true|false]`,
		func(line string) (stop bool) {
			if line != "" {
				val, err := strconv.ParseBool(line)
				if err != nil {
					fmt.Println(err)
					return
				}

				client.AllowInsecure(val)
			}

			return
		},
		nil})

	commander.Add(cmd.Command{
		"timeout",
		`timeout [duration]`,
		func(line string) (stop bool) {
			if line != "" {
				val, err := time.ParseDuration(line)
				if err != nil {
					fmt.Println(err)
					return
				}

				client.SetTimeout(val)
			}

			return
		},
		nil})

	commander.Add(cmd.Command{"http",
		`
                http [-get|-post|-head|-delete] url data
                `,
		func(line string) (stop bool) {
			args := args.ParseArgs(line)

			if len(args.Arguments) < 1 {
				fmt.Println("missing url")
				return
			}

			url := args.Arguments[0]
			data := ""

			if len(args.Arguments) > 1 {
				data = strings.Join(args.Arguments[1:], " ")
			}

			params := make(simplejson.Bag)
			headers := make(map[string]string)

			var res *httpclient.HttpResponse
			var err error

			if len(args.Options) == 0 { // no options
				args.Options["get"] = "true"
			}

			for !interrupted {
				if _, ok := args.Options["get"]; ok {
					if len(data) > 0 {
						fmt.Println("can't GET with data")
						return
					}
					res, err = client.Get(url, params, headers)
				} else if _, ok := args.Options["head"]; ok {
					if len(data) > 0 {
						fmt.Println("can't HEAD with data")
						return
					}
					res, err = client.Head(url, params, headers)
				} else if _, ok := args.Options["delete"]; ok {
					if len(data) > 0 {
						fmt.Println("can't DELETE with data")
						return
					}
					res, err = client.Delete(url, headers)
				} else if _, ok := args.Options["post"]; ok {
					res, err = client.Post(url, bytes.NewReader([]byte(data)), headers)
				}

				if err != nil {
					fmt.Println("REQUEST FAILED:", err)
					break
				}

				body := res.Content()
				fmt.Println(string(body))

				if pattern, ok := args.Options["wait"]; ok {
					match, err := regexp.Match(pattern, body)
					if err != nil {
						fmt.Println("MATCH ERROR:", err)
						break
					}

					if !match && !interrupted {
						time.Sleep(time.Second)
						continue
					}
				}

				break
			}

			interrupted = false
			return
		},
		nil})

	commander.Add(cmd.Command{
		"exit",
		`exit script`,
		func(line string) (stop bool) {
			fmt.Println("goodbye!")
			return true
		},
		nil})

	commander.CmdLoop()
}
