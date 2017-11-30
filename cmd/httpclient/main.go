package main

import (
	"github.com/gobs/args"
	"github.com/gobs/cmd"
	"github.com/gobs/httpclient"
	"github.com/gobs/jsonpath"
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
	completion_words = []string{}
	env              = map[string]string{}

	reFieldValue = regexp.MustCompile(`(\w[\d\w-]*)(=(.*))?`) // field-name=value
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

func request(client *httpclient.HttpClient, method, params string, print bool) *httpclient.HttpResponse {
	env["error"] = ""
	env["body"] = ""

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
		env["error"] = err.Error()
	}

	body := res.Content()
	if len(body) > 0 && print {
		if strings.Contains(res.Header.Get("Content-Type"), "json") {
			jbody, err := simplejson.LoadBytes(body)
			if err != nil {
				fmt.Println(err)
			} else {
				printJson(jbody.Data())
			}
		} else {
			fmt.Println(string(body))
		}
	}

	env["body"] = string(body)
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

func printJson(v interface{}) {
	fmt.Println(simplejson.MustDumpString(v, simplejson.Indent("  ")))
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

	commander.Vars = env
	commander.SetVar("print", true)

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
			res := request(client, "head", line, false)
			if res != nil {
				printJson(res.Header)
			}
			return
		},
		nil})

	commander.Add(cmd.Command{"get",
		`
                get [url-path] [short-data]
                `,
		func(line string) (stop bool) {
			request(client, "get", line, commander.GetBoolVar("print"))
			return
		},
		nil})

	commander.Add(cmd.Command{"post",
		`
                post [url-path] [short-data]
                `,
		func(line string) (stop bool) {
			request(client, "post", line, commander.GetBoolVar("print"))
			return
		},
		nil})

	commander.Add(cmd.Command{"put",
		`
                put [url-path] [short-data]
                `,
		func(line string) (stop bool) {
			request(client, "put", line, commander.GetBoolVar("print"))
			return
		},
		nil})

	commander.Add(cmd.Command{"delete",
		`
                delete [url-path] [short-data]
                `,
		func(line string) (stop bool) {
			request(client, "delete", line, commander.GetBoolVar("print"))
			return
		},
		nil})

	commander.Add(cmd.Command{"json",
		`
                json field1=value1 field2=value... // json object
                json [value, value...]             // json array
                `,
		func(line string) (stop bool) {
			var res interface{}
			jsonmap := true

			if strings.HasPrefix(line, "[") {
				jsonmap = false

				line = line[1:]
				if strings.HasSuffix(line, "]") {
					line = line[:len(line)-1]
				}
				line = strings.TrimSpace(line)
			}

			fields := args.GetArgs(line)

			parseValue := func(v string) (interface{}, error) {
				switch {
				case strings.HasPrefix(v, "{") || strings.HasPrefix(v, "["):
					j, err := simplejson.LoadString(v)
					if err != nil {
						return nil, fmt.Errorf("error parsing %q", v)
					} else {
						return j.Data(), nil
					}

				case v == "true":
					return true, nil

				case v == "false":
					return false, nil

				case v == "null":
					return nil, nil

				default:
					return v, nil
				}
			}

			if jsonmap {
				var err error
				mres := map[string]interface{}{}

				for _, f := range fields {
					matches := reFieldValue.FindStringSubmatch(f)
					if len(matches) > 0 { // [field=value field =value value]
						name, value := matches[1], matches[3]
						mres[name], err = parseValue(value)

						if err != nil {
							fmt.Println(err)
							env["error"] = err.Error()
							return
						}
					} else {
						fmt.Println("invalid name=value pair:", f)
						env["error"] = "invalid name=value pair"
						return
					}
				}

				res = mres
			} else {
				var ares []interface{}

				for _, f := range fields {
					v, err := parseValue(f)
					if err != nil {
						fmt.Println(err)
						env["error"] = err.Error()
						return
					}

					ares = append(ares, v)
				}
			}

			if commander.GetBoolVar("print") {
				printJson(res)
			}

			env["error"] = ""
			env["json"] = unquote(simplejson.MustDumpString(res))
			return
		},
		nil})

	commander.Add(cmd.Command{
		"jsonpath",
		`jsonpath [-e] [-c] path {json}`,
		func(line string) (stop bool) {
			var joptions jsonpath.ProcessOptions

			options, line := args.GetOptions(line)
			for _, o := range options {
				if o == "-e" || o == "--enhanced" {
					joptions |= jsonpath.Enhanced
				} else if o == "-c" || o == "--collapse" {
					joptions |= jsonpath.Collapse
				} else {
					line = "" // to force an error
					break
				}
			}

			parts := args.GetArgsN(line, 2)
			if len(parts) != 2 {
				fmt.Println("use: jsonpath [-e|--enhanced] path {json}")
				env["error"] = "invalid-usage"
				return
			}

			path := parts[0]
			if !strings.HasPrefix(path, "$.") {
				path = "$." + path
			}

			jbody, err := simplejson.LoadString(parts[1])
			if err != nil {
				fmt.Println("json:", err)
				env["error"] = err.Error()
				return
			}

			jp := jsonpath.NewProcessor()
			if !jp.Parse(path) {
				env["error"] = fmt.Sprintf("failed to parse %q", path)
				return // syntax error
			}

			res := jp.Process(jbody, joptions)
			if commander.GetBoolVar("print") {
				printJson(res)
			}
			env["error"] = ""
			env["json"] = unquote(simplejson.MustDumpString(res))
			return
		},
		nil})

	commander.Add(cmd.Command{
		"format",
		`format object`,
		func(line string) (stop bool) {
			jbody, err := simplejson.LoadString(line)
			if err != nil {
				fmt.Println("json:", err)
				env["error"] = err.Error()
				return
			}

			printJson(jbody.Data())
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
