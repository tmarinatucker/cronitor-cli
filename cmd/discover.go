package cmd

import (
	"runtime"
	"fmt"
	"encoding/json"
	"io/ioutil"
	"strings"
	"github.com/spf13/cobra"
	"errors"
	"net/http"
	"time"
	"log"
)

type Rule struct {
	RuleType string `json:"rule_type"`
	Value string `json:"value"`
	TimeUnit string `json:"time_unit,omitempty"`
	GraceSeconds uint `json:"grace_seconds,omitempty"`
}

type Monitor struct {
	Name string `json:"name"`
	Key string `json:"key"`
	Rules []Rule `json:"rules"`
	Tags []string `json:"tags"`
	Code string
}

type Line struct {
	Name           string
	FullLine       string
	LineNumber     int
	CronExpression string
	CommandToRun   string
	Code           string
	IsMonitorable  bool
	Mon				Monitor
}





var discoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "Automatically find cron jobs and attach Cronitor monitoring",
	Long: ``,
	Args: func(cmd *cobra.Command, args []string) error {
		// if len(args) < 2 {
		// 	return errors.New("A unique monitor code and cli command are required")
		// }

		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		if runtime.GOOS == "windows" {
			panic(errors.New("sorry, job discovery is not available on Windows"))
		}

		cronPath := "/etc/crontab"
		if len(args) > 0 {
			cronPath = args[0]
		}
		crontabLines := parseCrontabFile(cronPath)

		// construct JSON payload
		var monitors map[string]*Monitor
		for _, line := range crontabLines {
			if !line.isMonitorable {
				continue
			}

			rules := []Rule{createRule(line.CronExpression)}
			name := createName(line.CommandToRun)
			key := createKey(line.CommandToRun, line.CronExpression)

			line.Mon = Monitor{name, key, rules, []string{"tags", "are", "cool"}, line.Code}
			monitors[key] = &line.Mon
		}

		monitors = putMonitors(monitors)

		// Re-write crontab lines with new/updated monitoring
		var crontabOutput []string
		for idx, line := range crontabLines {
			crontabOutput[idx] = createCrontabLine(line)
		}

		crontabFile := strings.Join(crontabOutput, "\n")
		
		// Compose internal state back into a crontab file, adding/updating Cronitor wrapping
		fmt.Print(crontabFile)
	},
}

func putMonitors(monitors map[string]*Monitor) map[string]*Monitor {
	var monitorsArray []*Monitor
	for _, value := range monitors {
		monitorsArray = append(monitorsArray, value)
	}

	response := doPut("https://cronitor.link/v3/monitors", string(json.Marshal(monitorsArray)))
	var responseMonitors []Monitor

	json.Unmarshal(response, &responseMonitors)
	for _, value := range responseMonitors {
		monitors[value.Key].Code = value.Code
	}

	return monitors
}

func createCrontabLine(line Line) string {
	if !line.IsMonitorable || len(line.Code) > 0 {
		// If a cronitor integration already existed on the line we have nothing else here to change
		return line.FullLine
	}

	var lineParts []string
	lineParts = append(lineParts, line.CronExpression)

	if len(line.Mon.Key) > 0 {
		lineParts = append(lineParts, "cronitor exec")
		lineParts = append(lineParts, line.Mon.Code)
	}

	if len(line.CommandToRun) > 0 {
		lineParts = append(lineParts, line.CommandToRun)
	}

	return strings.Join(lineParts, " ")
}

func parseCrontabFile(cronPath string) []Line {
	bytes, err := ioutil.ReadFile(cronPath)
	if err != nil {
		panic(err)
	}
	lines := strings.Split(string(bytes), "\n")

	var crontabLines []Line
	for lineNumber, line := range lines {
		var cronExpression string
		var command []string

		// Skip the current line if it's a comment
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			// todo verbose message this
			continue
		}

		// If a manual integration was previously done, skip
		if strings.Contains(line, "cronitor.io") || strings.Contains(line, "cronitor.link") {
			// todo verbose message this -- previous integration on line number lineNo
			continue
		}

		// Split line by whitespace
		splitLine := strings.Fields(line)
		if strings.HasPrefix(splitLine[0], "@") {
			if strings.HasPrefix(splitLine[0], "@reboot") {
				// todo verbose message -- @reboot aren't scheduled jobs
			} else {
				cronExpression = splitLine[0]
				command = splitLine[1:]
			}
		} else if len(splitLine) >= 6 {
			cronExpression = strings.Join(splitLine[0:5], " ")
			command = splitLine[5:]
		}

		// Create an Line struct with details for this line (even if it does not have a parsed command
		entry := Line{}
		entry.CronExpression = cronExpression
		entry.FullLine = line
		entry.LineNumber = lineNumber

		// If this job is already being wrapped by the Cronitor client, read current code
		// Expects a wrapped command to look like: cronitor exec d3x0 /path/to/cmd.sh
		if len(command) > 0 && command[1] == "cronitor" && command[2] == "exec" {
			entry.Code = command[3]
			command = command[3:]
		}

		entry.CommandToRun = strings.Join(command, " ")
		entry.IsMonitorable = len(entry.CommandToRun) > 0 && len(entry.CronExpression) > 0

		fmt.Println(cronExpression, command)
		crontabLines = append(crontabLines, entry)
	}

	return crontabLines
}

func createName(CommandToRun string) string {
	return CommandToRun
}

func createKey(CommandToRun string, CronExpression string) string {
	return "keykey"
}

func createRule(cronExpression string) Rule {
	var rule Rule
	if strings.HasPrefix(cronExpression, "@yearly") {
		rule = Rule{"complete_ping_not_received", "365", "days", 86400}
	} else if strings.HasPrefix(cronExpression, "@monthly") {
		rule =  Rule{"complete_ping_not_received", "31", "days", 86400}
	} else if strings.HasPrefix(cronExpression, "@weekly") {
		rule =  Rule{"complete_ping_not_received", "7", "days", 86400}
	} else if strings.HasPrefix(cronExpression, "@daily") {
		rule =  Rule{"complete_ping_not_received", "24", "hours", 3600}
	} else if strings.HasPrefix(cronExpression, "@hourly") {
		rule =  Rule{"complete_ping_not_received", "1", "hours", 600}
	} else {
		rule =  Rule{"not_on_schedule", cronExpression, "", 0}
	}

	return rule
}

func doPut(url string, body string) []byte {
	client := &http.Client{}
	request, err := http.NewRequest("PUT", url, strings.NewReader(body))
	request.ContentLength = int64(len(body))
	response, err := client.Do(request)
	if err != nil {
		log.Fatal(err)
		return make([]byte, 0)
	}

	defer response.Body.Close()
	contents, err := ioutil.ReadAll(response.Body)
	if err != nil {
		log.Fatal(err)
	}

	return contents
}

func init() {
	RootCmd.AddCommand(discoverCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// discoverCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// discoverCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}