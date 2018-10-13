package main

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"syscall"

	"github.com/beevik/cmd"
	cloudflare "github.com/cloudflare/cloudflare-go"
	"golang.org/x/crypto/ssh/terminal"
)

var (
	api    *cloudflare.API
	zoneID string
	cmds   *cmd.Tree
)

func init() {
	root := cmd.NewTree("cf")
	root.AddCommand(cmd.Command{
		Name:        "help",
		Description: "Display help for a command.",
		Usage:       "help [<command>]",
		Data:        cmdHelp,
	})
	root.AddCommand(cmd.Command{
		Name:        "zone",
		Brief:       "Set active zone",
		Description: "Set the active zone used by all future DNS commands.",
		Usage:       "zone <name>",
		Data:        cmdSetZone,
	})
	root.AddCommand(cmd.Command{
		Name:        "list",
		Brief:       "List all domains",
		Description: "List all domains in the currently active zone.",
		Usage:       "list [<type>]",
		Data:        cmdListDomains,
	})
	root.AddCommand(cmd.Command{
		Name:  "addr",
		Brief: "Add or modify an address record",
		Description: "Add or modify an IPv4 address (type A) DNS record in " +
			"the currently active zone.",
		Usage: "addr <address> <ip>",
		Data:  cmdAddr,
	})
	root.AddCommand(cmd.Command{
		Name:  "quit",
		Brief: "Quit the application",
		Usage: "quit",
		Data:  cmdQuit,
	})

	cmds = root
}

func main() {
	var err error
	api, err = getAPI()
	if err != nil {
		log.Fatal(err)
	}

	for {
		line, err := readString("cf> ")
		if err != nil {
			break
		}

		var c cmd.Selection
		if line != "" {
			c, err = cmds.Lookup(line)
			switch {
			case err == cmd.ErrNotFound:
				fmt.Println("Command not found.")
				continue
			case err == cmd.ErrAmbiguous:
				fmt.Println("Command ambiguous.")
				continue
			case err != nil:
				fmt.Printf("Error: %v\n", err)
				continue
			}
		}

		if c.Command == nil {
			continue
		}
		if c.Command.Data == nil && c.Command.Subtree != nil {
			displayCommands(c.Command.Subtree, nil)
			continue
		}

		handler := c.Command.Data.(func(cmd.Selection) error)
		err = handler(c)
		if err != nil {
			break
		}
	}
}

func cmdQuit(c cmd.Selection) error {
	return errors.New("Exiting program")
}

func cmdHelp(c cmd.Selection) error {
	switch {
	case len(c.Args) == 0:
		displayCommands(cmds, nil)
	default:
		s, err := cmds.Lookup(strings.Join(c.Args, " "))
		if err != nil {
			fmt.Printf("%v\n", err)
		} else {
			switch {
			case s.Command.Subtree != nil:
				displayCommands(s.Command.Subtree, s.Command)
			default:
				if s.Command.Usage != "" {
					fmt.Printf("Usage: %s\n\n", s.Command.Usage)
				}
				switch {
				case s.Command.Description != "":
					fmt.Printf("Description:\n%s\n\n", indentWrap(3, s.Command.Description))
				case s.Command.Brief != "":
					fmt.Printf("Description:\n%s.\n\n", indentWrap(3, s.Command.Brief))
				}
				if s.Command.Shortcuts != nil {
					switch {
					case len(s.Command.Shortcuts) > 1:
						fmt.Printf("Shortcuts: %s\n\n", strings.Join(s.Command.Shortcuts, ", "))
					default:
						fmt.Printf("Shortcut: %s\n\n", s.Command.Shortcuts[0])
					}
				}
			}
		}
	}
	return nil
}

func cmdSetZone(c cmd.Selection) error {
	if len(c.Args) < 1 {
		displayUsage(c.Command)
		return nil
	}

	var err error
	zoneID, err = api.ZoneIDByName(c.Args[0])
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return nil
	}

	fmt.Printf("Active zone set to %v.\n", c.Args[0])
	return nil
}

func cmdListDomains(c cmd.Selection) error {
	if zoneID == "" {
		fmt.Println("Zone not set.")
		return nil
	}

	typ := ""
	if len(c.Args) > 0 {
		typ = strings.ToUpper(c.Args[0])
	}

	recs, err := api.DNSRecords(zoneID, cloudflare.DNSRecord{Type: typ})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return nil
	}

	widthType := 0
	widthName := 0
	for _, rec := range recs {
		if len(rec.Name) > widthName {
			widthName = len(rec.Name)
		}
		if len(rec.Type) > widthType {
			widthType = len(rec.Type)
		}
	}

	for _, rec := range recs {
		fmt.Printf("%-*s %-*s %s\n", widthType, rec.Type, widthName, rec.Name, rec.Content)
	}

	return nil
}

func cmdAddr(c cmd.Selection) error {
	if zoneID == "" {
		fmt.Println("Zone net set.")
		return nil
	}

	if len(c.Args) != 2 {
		displayUsage(c.Command)
		return nil
	}

	addr := c.Args[0]
	ip := c.Args[1]

	recs, err := api.DNSRecords(zoneID, cloudflare.DNSRecord{Type: "A", Name: addr})
	if err == nil && len(recs) > 0 {
		rr := recs[0]
		if rr.Content != ip {
			rr.Content = ip
			rr.Proxied = false
			err = api.UpdateDNSRecord(zoneID, rr.ID, rr)
		}
	} else {
		rr := cloudflare.DNSRecord{
			Type:    "A",
			Name:    addr,
			Content: ip,
			Proxied: false,
			TTL:     1,
		}
		_, err = api.CreateDNSRecord(zoneID, rr)
	}

	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return nil
	}

	fmt.Println("Address record updated.")
	return nil
}

func displayUsage(c *cmd.Command) {
	if c.Usage != "" {
		fmt.Printf("Usage: %s\n", c.Usage)
	}
}

func displayCommands(commands *cmd.Tree, c *cmd.Command) {
	fmt.Printf("%s commands:\n", commands.Title)
	for _, c := range commands.Commands {
		if c.Brief != "" {
			fmt.Printf("    %-15s  %s\n", c.Name, c.Brief)
		}
	}
	fmt.Println()

	if c != nil && c.Shortcuts != nil && len(c.Shortcuts) > 0 {
		switch {
		case len(c.Shortcuts) > 1:
			fmt.Printf("Shortcuts: %s\n\n", strings.Join(c.Shortcuts, ", "))
		default:
			fmt.Printf("Shortcut: %s\n\n", c.Shortcuts[0])
		}
	}
}

func readString(prompt string) (string, error) {
	fmt.Printf(prompt)

	reader := bufio.NewReader(os.Stdin)
	text, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(text, "\n"), nil
}

func readHiddenString(prompt string) (string, error) {
	fmt.Printf(prompt)

	bytes, err := terminal.ReadPassword(int(syscall.Stdin))
	if err != nil {
		return "", err
	}
	fmt.Println()

	return string(bytes), nil
}

func getAPI() (*cloudflare.API, error) {
	key := os.Getenv("CLOUDFLARE_KEY")
	email := os.Getenv("CLOUDFLARE_EMAIL")

	var err error
	if email == "" {
		email, err = readString("Enter cloudflare account email: ")
		if err != nil {
			return nil, err
		}
	}

	if key == "" {
		key, err = readHiddenString("Enter cloudflare API key: ")
		if err != nil {
			return nil, err
		}
	}

	return cloudflare.New(key, email)
}

func getDNSRecords() ([]cloudflare.DNSRecord, error) {
	if zoneID == "" {
		return nil, errors.New("Zone not set")
	}

	return api.DNSRecords(zoneID, cloudflare.DNSRecord{})
}

func indentWrap(indent int, s string) string {
	ss := strings.Fields(s)
	if len(ss) == 0 {
		return ""
	}

	counts := make([]int, 0)
	count := 1
	l := indent + len(ss[0])
	for i := 1; i < len(ss); i++ {
		if l+1+len(ss[i]) < 80 {
			count++
			l += 1 + len(ss[i])
			continue
		}

		counts = append(counts, count)
		count = 1
		l = indent + len(ss[i])
	}
	counts = append(counts, count)

	var lines []string
	i := 0
	for _, c := range counts {
		line := strings.Repeat(" ", indent) + strings.Join(ss[i:i+c], " ")
		lines = append(lines, line)
		i += c
	}

	return strings.Join(lines, "\n")
}
