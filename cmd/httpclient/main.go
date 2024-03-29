package main

import (
	"github.com/gobs/args"
	"github.com/gobs/cmd"
	"github.com/gobs/cmd/plugins/controlflow"
	"github.com/gobs/cmd/plugins/json"
	"github.com/gobs/cmd/plugins/stats"
	"github.com/gobs/httpclient"
	"github.com/gobs/simplejson"
	"github.com/google/uuid"

	"golang.org/x/net/publicsuffix"
	"net/http"

	//"net/http/cookiejar"
	"github.com/juju/persistent-cookiejar"

	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	HISTORY_FILE = ".httpclient_history"
	COOKIE_FILE  = ".httpclient_cookies"
)

var (
	reFieldValue = regexp.MustCompile(`(\w[\d\w-]*)(=(.*))?`) // field-name=value
)

func request(cmd *cmd.Cmd, client *httpclient.HttpClient, method, params string, print, trace bool) *httpclient.HttpResponse {
	cmd.SetVar("body", "")
	cmd.SetVar("status", "")
	cmd.SetVar("error", "")

	// [-options...] "path" {body}

	options := []httpclient.RequestOption{httpclient.Method(method)}

	var rtrace *httpclient.RequestTrace

	if trace {
		rtrace = &httpclient.RequestTrace{}
		options = append(options, httpclient.Trace(rtrace.NewClientTrace(true)))
	}

	args := args.ParseArgs(params, args.InfieldBrackets())

	if len(args.Arguments) > 0 {
		options = append(options, client.Path(args.Arguments[0]))
	}

	if len(args.Arguments) > 1 {
		data := strings.Join(args.Arguments[1:], " ")
		options = append(options, httpclient.Body(strings.NewReader(data)))
	}

	if len(args.Options) > 0 {
		options = append(options, httpclient.StringParams(args.Options))
	}

	res, err := client.SendRequest(options...)
	if rtrace != nil {
		rtrace.Done()
	}
	if err == nil {
		cmd.SetVar("status", res.Status)
		err = res.ResponseError()
	}
	if err != nil {
		if print {
			fmt.Println("ERROR:", err)
		}

		cmd.SetVar("error", err)
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

	//cookies := res.Cookies()
	//if len(cookies) > 0 {
	//        client.Cookies = cookies
	//}

	cmd.SetVar("body", string(body))
	if rtrace != nil {
		cmd.SetVar("rtrace", simplejson.MustDumpString(rtrace))
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
	//var interrupted bool
	var logBody bool
	var client = httpclient.NewHttpClient("")

	client.UserAgent = "httpclient/0.1"

	commander := &cmd.Cmd{
		HistoryFile: HISTORY_FILE,
		EnableShell: true,
		//Interrupt:   func(sig os.Signal) bool { interrupted = true; return false },
	}

	commander.Init(controlflow.Plugin, json.Plugin, stats.Plugin)

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
					commander.SetVar("body", client.BaseURL)
					return
				}
			}

			fmt.Println("base", client.BaseURL)
			commander.SetVar("body", client.BaseURL)
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
		`verbose [true|false|body]`,
		func(line string) (stop bool) {
			if line == "body" {
				if !logBody {
					client.StartLogging(true, true, true)
					logBody = true
				}
			} else if line != "" {
				val, err := strconv.ParseBool(line)
				if err != nil {
					fmt.Println(err)
					return
				}

				client.Verbose = val

				if !val && logBody {
					client.StopLogging()
					logBody = false
				}
			}

			fmt.Println("Verbose", client.Verbose)
			if logBody {
				fmt.Println("Logging Request/Response body")
			}
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
				value := unquote(parts[1])

				if value == "" {
					delete(client.Headers, name)
				} else {
					client.Headers[name] = value
				}

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
			res := request(commander, client, "head", line, false, commander.GetBoolVar("trace"))
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
			request(commander, client, "get", line, commander.GetBoolVar("print"), commander.GetBoolVar("trace"))
			return
		},
		nil})

	commander.Add(cmd.Command{"post",
		`
                post [url-path] [short-data]
                `,
		func(line string) (stop bool) {
			request(commander, client, "post", line, commander.GetBoolVar("print"), commander.GetBoolVar("trace"))
			return
		},
		nil})

	commander.Add(cmd.Command{"put",
		`
                put [url-path] [short-data]
                `,
		func(line string) (stop bool) {
			request(commander, client, "put", line, commander.GetBoolVar("print"), commander.GetBoolVar("trace"))
			return
		},
		nil})

	commander.Add(cmd.Command{"delete",
		`
                delete [url-path] [short-data]
                `,
		func(line string) (stop bool) {
			request(commander, client, "delete", line, commander.GetBoolVar("print"), commander.GetBoolVar("trace"))
			return
		},
		nil})

	commander.Add(cmd.Command{"jwt",
		`
                jwt token
                `,
		func(line string) (stop bool) {
			parts := strings.Split(line, ".")
			if len(parts) != 3 {
				fmt.Println("not a JWT token")
			}

			decoded, err := base64.RawStdEncoding.DecodeString(parts[1])
			if err != nil {
				fmt.Println(err)
			} else {
				fmt.Println(string(decoded))
				commander.SetVar("body", string(decoded))
			}
			return
		},
		nil})

	commander.Add(cmd.Command{"cookiejar",
		`
                cookiejar [--add|--delete|--save|domain]
                `,
		func(line string) (stop bool) {
			if line == "--add" {
				if client.GetCookieJar() != nil {
					fmt.Println("you already have a cookie jar")
					return
				}

				jar, err := cookiejar.New(&cookiejar.Options{
					PublicSuffixList: publicsuffix.List,
					Filename:         COOKIE_FILE,
				})
				if err != nil {
					fmt.Println("cannot create cookiejar:", err)
					commander.SetVar("error", err)
				}

				client.SetCookieJar(jar)
				fmt.Println("cookiejar added")
			} else if line == "--delete" || line == "--remove" {
				client.SetCookieJar(nil)
				fmt.Println("cookiejar removed")
			} else if line == "--save" {
				if jar := client.GetCookieJar(); jar != nil {
					jar.(*cookiejar.Jar).Save()
				}
			} else if strings.HasPrefix(line, "-") {
				fmt.Println("invalid option", line)
				fmt.Println("usage: cookiejar [--add|--delete|--save]")
			} else {
				if client.GetCookieJar() == nil {
					fmt.Println("no cookiejar")
					return
				}

				if line == "" {
					fmt.Println("usage: cookiejar baseurl")
					return
				}

				u, err := url.Parse(line)
				if err != nil {
					fmt.Println(err)
					commander.SetVar("error", err)
					return
				}

				cookies := client.GetCookieJar().Cookies(u)
				if len(cookies) == 0 {
					fmt.Println("no cookies in the cookiejar")
					return
				}

				fmt.Println("Cookies:")
				for _, cookie := range cookies {
					//fmt.Println(simplejson.MustDumpString(cookie, simplejson.Indent(" ")))
					fmt.Printf("  %v: %v\n", cookie.Name, cookie.Value)
				}
			}

			return
		},
		nil})

	commander.Add(cmd.Command{"serve",
		`
                serve [[host]:port] [dir]
                `,
		func(line string) (stop bool) {
			port := ":3000"
			dir := "."

			parts := strings.Fields(line)
			if len(parts) > 2 {
				fmt.Println("too many arguments")
				fmt.Println()
				fmt.Println("usage: serve [[host]:port] [dir]")
				return
			}

			for _, p := range parts {
				if strings.Contains(p, ":") {
					port = p
				} else {
					dir = p
				}
			}

			fmt.Printf("Serving directory %q on port %v\n", dir, port)
			if err := http.ListenAndServe(port, http.FileServer(http.Dir(dir))); err != nil {
				fmt.Println(err)
			}

			return
		},
		nil})

	commander.Add(cmd.Command{"uuid",
		`
                uuid [1|4]
                `,
		func(line string) (stop bool) {
			gen := uuid.NewUUID // type 1
			if line == "4" {    // type 4
				gen = uuid.NewRandom
			}

			uid, err := gen()
			if err != nil {
				fmt.Println(err)
				commander.SetVar("error", err)
				commander.SetVar("uuid", "")
			} else {
				fmt.Println(uid.String())
				commander.SetVar("error", "")
				commander.SetVar("uuid", uid.String())
			}

			return
		},
		nil})

	commander.Commands["set"] = commander.Commands["var"]

	if len(os.Args) > 1 && os.Args[1] == "serve" {
		commander.OneCmd(strings.Join(os.Args[1:], " "))
		return
	}

	switch len(os.Args) {
	case 1: // program name only
		break

	case 2: // one arg - expect URL or @filename
		cmd := os.Args[1]
		if !strings.HasPrefix(cmd, "@") {
			cmd = "base " + cmd
		}

		if commander.OneCmd(cmd) {
			return
		}

	case 3:
		if os.Args[1] == "-script" || os.Args[1] == "--script" {
			cmd := "@" + os.Args[2]
			commander.OneCmd(cmd)
		} else {
			fmt.Println("usage:", os.Args[0], "[{base-url} | @{script-file} | -script {script-file}]")
		}

		return

	default:
		fmt.Println("usage:", os.Args[0], "[{base-url} | @{script-file} | -script {script-file}]")
		return
	}

	commander.CmdLoop()
}
