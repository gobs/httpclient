package main

import (
	"github.com/gobs/args"
	"github.com/gobs/cmd"
	"github.com/gobs/httpclient"
	"github.com/gobs/simplejson"

	"fmt"
	"net/url"
	"os"
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

func request(client *httpclient.HttpClient, method, params string) *httpclient.HttpResponse {
	options := []httpclient.RequestOption{client.Method(method)}
	args := args.ParseArgs(params)

	if len(args.Arguments) > 0 {
		options = append(options, client.Path(args.Arguments[0]))
	}

	if len(args.Arguments) > 1 {
		data := strings.Join(args.Arguments[1:], " ")
		options = append(options, client.Body(strings.NewReader(data)))
	}

	res, err := client.SendRequest(options...)
	if err == nil {
		err = res.ResponseError()
	}
	if err != nil {
		fmt.Println("ERROR:", err)
	}

	body := res.Content()
	if len(body) > 0 {
		if strings.Contains(res.Header.Get("Content-Type"), "json") {
			jbody, err := simplejson.LoadBytes(body)
			if err != nil {
				fmt.Println(err)
			} else {
				fmt.Println(simplejson.MustDumpString(jbody.Data(), simplejson.Indent("  ")))
			}
		} else {
			fmt.Println(string(body))
		}
	}

	return res
}

func headerName(s string) string {
	s = strings.ToLower(s)
	parts := strings.Split(s, "-")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[0:1]) + p[1:]
		}
	}
	return strings.Join(parts, "-")
}

func unquote(s string) string {
	if res, err := strconv.Unquote(s); err == nil {
		return res
	}

	return s
}

func main() {
	var interrupted bool
	var client = httpclient.NewHttpClient("")

	client.UserAgent = "httpclient/0.1"

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

			fmt.Println("base", client.BaseURL)
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

			// assume if there is a transport, it's because we set AllowInsecure
			fmt.Println("insecure", client.GetTransport() != nil)

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

			fmt.Println("timeout", client.GetTimeout())
			return
		},
		nil})

	commander.Add(cmd.Command{
		"verbose",
		`verbose [true|false]`,
		func(line string) (stop bool) {
			if line != "" {
				val, err := strconv.ParseBool(line)
				if err != nil {
					fmt.Println(err)
					return
				}

				client.Verbose = val
			}

			fmt.Println("Verbose", client.Verbose)
			return
		},
		nil})

	commander.Add(cmd.Command{
		"agent",
		`agent user-agent-string`,
		func(line string) (stop bool) {
			if line != "" {
				client.UserAgent = line
			}

			fmt.Println("User-Agent:", client.UserAgent)
			return
		},
		nil})

	commander.Add(cmd.Command{
		"header",
		`header [name [value]]`,
		func(line string) (stop bool) {
			if line == "" {
				if len(client.Headers) == 0 {
					fmt.Println("No headers")
				} else {
					fmt.Println("Headers:")
					for k, v := range client.Headers {
						fmt.Printf("  %v: %v\n", k, v)
					}
				}

				return
			}

			parts := args.GetArgsN(line, 2)
			name := headerName(parts[0])

			if len(parts) == 2 {
				client.Headers[name] = unquote(parts[1])
			}

			fmt.Printf("%v: %v\n", name, client.Headers[name])
			return
		},
		nil})

	commander.Add(cmd.Command{"get",
		`
                get [url-path] [short-data]
                `,
		func(line string) (stop bool) {
			request(client, "get", line)
			return
		},
		nil})

	commander.Add(cmd.Command{"head",
		`
                head [url-path] [short-data]
                `,
		func(line string) (stop bool) {
			res := request(client, "head", line)
			if res != nil {
				fmt.Println(simplejson.MustDumpString(res.Header, simplejson.Indent("  ")))
			}
			return
		},
		nil})

	commander.Add(cmd.Command{"post",
		`
                post [url-path] [short-data]
                `,
		func(line string) (stop bool) {
			request(client, "post", line)
			return
		},
		nil})

	commander.Add(cmd.Command{"put",
		`
                put [url-path] [short-data]
                `,
		func(line string) (stop bool) {
			request(client, "put", line)
			return
		},
		nil})

	commander.Add(cmd.Command{"delete",
		`
                delete [url-path] [short-data]
                `,
		func(line string) (stop bool) {
			request(client, "delete", line)
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

	switch len(os.Args) {
	case 1: // program name only
		break

	case 2: // one arg - expect URL
		commander.OneCmd("base " + os.Args[1])

	default:
		fmt.Println("usage:", os.Args[0], "[base-url]")
		return
	}

	commander.CmdLoop()
}
