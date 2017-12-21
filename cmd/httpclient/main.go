package main

import (
	"github.com/gobs/args"
	"github.com/gobs/cmd"
	"github.com/gobs/cmd/plugins/controlflow"
	"github.com/gobs/cmd/plugins/json"
	"github.com/gobs/httpclient"
	"github.com/gobs/simplejson"

	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	reFieldValue = regexp.MustCompile(`(\w[\d\w-]*)(=(.*))?`) // field-name=value
)

func request(cmd *cmd.Cmd, client *httpclient.HttpClient, method, params string, print bool) *httpclient.HttpResponse {
	cmd.SetVar("error", "", true)
	cmd.SetVar("body", "", true)

	options := []httpclient.RequestOption{client.Method(method)}
	args := args.ParseArgs(params)

	if len(args.Arguments) > 0 {
		options = append(options, client.Path(args.Arguments[0]))
	}

	if len(args.Arguments) > 1 {
		data := strings.Join(args.Arguments[1:], " ")
		options = append(options, client.Body(strings.NewReader(data)))
	}

	if len(args.Options) > 0 {
		options = append(options, client.StringParams(args.Options))
	}

	res, err := client.SendRequest(options...)
	if err == nil {
		err = res.ResponseError()
	}
	if err != nil {
		fmt.Println("ERROR:", err)
		cmd.SetVar("error", err, true)
	}

	body := res.Content()
	if len(body) > 0 && print {
		if strings.Contains(res.Header.Get("Content-Type"), "json") {
			jbody, err := simplejson.LoadBytes(body)
			if err != nil {
				fmt.Println(err)
			} else {
				json.PrintJson(jbody.Data())
			}
		} else {
			fmt.Println(string(body))
		}
	}

	cmd.SetVar("body", string(body), true)
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
	if res, err := strconv.Unquote(strings.TrimSpace(s)); err == nil {
		return res
	}

	return s
}

func parseValue(v string) (interface{}, error) {
	switch {
	case strings.HasPrefix(v, "{") || strings.HasPrefix(v, "["):
		j, err := simplejson.LoadString(v)
		if err != nil {
			return nil, fmt.Errorf("error parsing %q", v)
		} else {
			return j.Data(), nil
		}

	case strings.HasPrefix(v, `"`):
		return strings.Trim(v, `"`), nil

	case strings.HasPrefix(v, `'`):
		return strings.Trim(v, `'`), nil

	case v == "":
		return v, nil

	case v == "true":
		return true, nil

	case v == "false":
		return false, nil

	case v == "null":
		return nil, nil

	default:
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return i, nil
		}
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f, nil
		}

		return v, nil
	}
}

func main() {
	var interrupted bool
	var client = httpclient.NewHttpClient("")

	client.UserAgent = "httpclient/0.1"

	commander := &cmd.Cmd{
		HistoryFile: ".httpclient_history",
		EnableShell: true,
		Interrupt:   func(sig os.Signal) bool { interrupted = true; return false },
	}

	commander.Init(controlflow.Plugin, json.Plugin)

	commander.SetVar("print", true, false)

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
				commander.SetPrompt(fmt.Sprintf("%v> ", client.BaseURL), 40)
				if !commander.GetBoolVar("print") {
					return
				}
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
		"timing",
		`timing [true|false]`,
		func(line string) (stop bool) {
			if line != "" {
				val, err := strconv.ParseBool(line)
				if err != nil {
					fmt.Println(err)
					return
				}

				commander.Timing = val
			}

			fmt.Println("Timing", commander.Timing)
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
				if !commander.GetBoolVar("print") {
					return
				}
			}

			fmt.Printf("%v: %v\n", name, client.Headers[name])
			return
		},
		nil})

	commander.Add(cmd.Command{"head",
		`
                head [url-path] [short-data]
                `,
		func(line string) (stop bool) {
			res := request(commander, client, "head", line, false)
			if res != nil {
				json.PrintJson(res.Header)
			}
			return
		},
		nil})

	commander.Add(cmd.Command{"get",
		`
                get [url-path] [short-data]
                `,
		func(line string) (stop bool) {
			request(commander, client, "get", line, commander.GetBoolVar("print"))
			return
		},
		nil})

	commander.Add(cmd.Command{"post",
		`
                post [url-path] [short-data]
                `,
		func(line string) (stop bool) {
			request(commander, client, "post", line, commander.GetBoolVar("print"))
			return
		},
		nil})

	commander.Add(cmd.Command{"put",
		`
                put [url-path] [short-data]
                `,
		func(line string) (stop bool) {
			request(commander, client, "put", line, commander.GetBoolVar("print"))
			return
		},
		nil})

	commander.Add(cmd.Command{"delete",
		`
                delete [url-path] [short-data]
                `,
		func(line string) (stop bool) {
			request(commander, client, "delete", line, commander.GetBoolVar("print"))
			return
		},
		nil})

	commander.Commands["set"] = commander.Commands["var"]

	switch len(os.Args) {
	case 1: // program name only
		break

	case 2: // one arg - expect URL or @filename
		cmd := os.Args[1]
		if !strings.HasPrefix(cmd, "@") {
			cmd = "base " + cmd
		}

		commander.OneCmd(cmd)

	default:
		fmt.Println("usage:", os.Args[0], "[base-url]")
		return
	}

	commander.CmdLoop()
}
