// Copyright 2018 Brett Vickers.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The cf tool allows you to view and manipulate DNS records stored in
// your Cloudflare account.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/beevik/cmd"
	cloudflare "github.com/cloudflare/cloudflare-go"
	"golang.org/x/term"
)

var (
	interactive  bool
	activeAPI    *cloudflare.API
	activeZoneID string
	cmds         *cmd.Tree
)

func init() {
	root := cmd.NewTree("Primary")
	root.AddCommand(cmd.Command{
		Name:        "help",
		Description: "Display help for a command.",
		Usage:       "help [<command>]",
		Data:        cmdHelp,
	})
	root.AddCommand(cmd.Command{
		Name:        "list",
		Brief:       "List all DNS records",
		Description: "List all DNS records in the currently active zone.",
		Usage:       "list [<type>]",
		Data:        cmdListDomains,
	})
	root.AddCommand(cmd.Command{
		Name:  "ip4",
		Brief: "Add or modify an IPv4 Address (type A) record",
		Description: "Add or modify an IPv4 address (type A) DNS record " +
			"in the currently active zone.",
		Usage: "ip4 <name> <address>",
		Data:  cmdIP4,
	})
	root.AddCommand(cmd.Command{
		Name:  "ip6",
		Brief: "Add or modify an IPv6 Address (type AAAA) record",
		Description: "Add or modify an IPv6 address (type AAAA) DNS record " +
			"in the currently active zone.",
		Usage: "ip6 <name> <address>",
		Data:  cmdIP6,
	})
	root.AddCommand(cmd.Command{
		Name:  "cname",
		Brief: "Add or modify a CNAME record",
		Description: "Add or modify a CNAME DNS record " +
			"in the currently active zone.",
		Usage: "cname <name> <address>",
		Data:  cmdCNAME,
	})
	root.AddCommand(cmd.Command{
		Name:  "txt",
		Brief: "Add or modify a text (type TXT) record",
		Description: "Add or modify a text (type TXT) DNS record " +
			"in the currently active zone.",
		Usage: "txt <name> <address>",
		Data:  cmdTXT,
	})
	root.AddCommand(cmd.Command{
		Name:  "add",
		Brief: "Add a DNS record",
		Description: "Add a DNS record of the requested type " +
			"in the currently active zone. The type must be one of the " +
			"allowed DNS record types (A, AAAA, CNAME, etc.). If the " +
			"content string has spaces, it must be enclosed in quotes. " +
			"This command always adds a new record if it succeeds, even if " +
			"there is already another record with the same name and type.",
		Usage: "add <type> <name> \"<content>\"",
		Data:  cmdAdd,
	})
	root.AddCommand(cmd.Command{
		Name:  "delete",
		Brief: "Delete DNS record(s)",
		Description: "Delete all DNS records matching the requested type " +
			"and name in the currently active zone. The type must be one " +
			"of the allowed DNS record types (A, AAAA, CNAME, etc.).",
		Usage: "delete <type> <name>",
		Data:  cmdDelete,
	})
	root.AddCommand(cmd.Command{
		Name:        "zone",
		Brief:       "Set active zone",
		Description: "Set the active zone used by all future commands.",
		Usage:       "zone <name>",
		Data:        cmdSetZone,
	})
	root.AddCommand(cmd.Command{
		Name:  "quit",
		Brief: "Quit the application",
		Usage: "quit",
		Data:  cmdQuit,
	})

	root.AddShortcut("?", "help")
	root.AddShortcut("l", "list")
	root.AddShortcut("ip", "ip4")
	cmds = root
}

func main() {
	args := os.Args[1:]
	interactive = len(args) == 0

	if interactive {
		runInteractive()
	} else {
		processCmd(fixupArgs(args))
	}
}

func runInteractive() {
	for {
		line, err := readString("cf> ")
		if err != nil {
			break
		}

		err = processCmd(line)
		if err != nil {
			break
		}
	}
}

func fixupArgs(args []string) string {
	newArgs := []string{}

	for _, a := range args {
		if strings.ContainsAny(a, " \t") {
			a = "\"" + a + "\""
		}
		newArgs = append(newArgs, a)
	}

	return strings.Join(newArgs, " ")
}

func processCmd(line string) error {
	var err error
	var c cmd.Selection
	if line != "" {
		c, err = cmds.Lookup(line)
		switch {
		case err == cmd.ErrNotFound:
			fmt.Println("Command not found.")
			return nil
		case err == cmd.ErrAmbiguous:
			fmt.Println("Command ambiguous.")
			return nil
		case err != nil:
			fmt.Printf("Error: %v\n", err)
			return nil
		}
	}

	if c.Command == nil {
		return nil
	}
	if c.Command.Data == nil && c.Command.Subtree != nil {
		c.Command.Subtree.DisplayCommands(os.Stdout)
		c.Command.DisplayShortcuts(os.Stdout)
		return nil
	}

	handler := c.Command.Data.(func(cmd.Selection) error)
	return handler(c)
}

func cmdQuit(c cmd.Selection) error {
	return errors.New("exiting program")
}

func cmdHelp(c cmd.Selection) error {
	err := cmds.DisplayHelp(os.Stdout, c.Args)
	if err != nil {
		fmt.Printf("%v.\n", err)
	}
	return nil
}

func cmdSetZone(c cmd.Selection) error {
	if len(c.Args) < 1 {
		c.Command.DisplayUsage(os.Stdout)
		return nil
	}

	api := getAPI()
	if api == nil {
		return nil
	}

	zoneID, err := api.ZoneIDByName(c.Args[0])
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return nil
	}

	activeZoneID = zoneID
	fmt.Printf("Active zone set to %v.\n", c.Args[0])
	return nil
}

func cmdListDomains(c cmd.Selection) error {
	zoneID := getZoneID()
	if zoneID == "" {
		return nil
	}

	api := getAPI()
	if api == nil {
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

func cmdIP4(c cmd.Selection) error {
	if len(c.Args) != 2 {
		c.Command.DisplayUsage(os.Stdout)
		return nil
	}

	name := c.Args[0]
	addr := c.Args[1]
	addOrUpdateRecord("A", name, addr)
	return nil
}

func cmdIP6(c cmd.Selection) error {
	if len(c.Args) != 2 {
		c.Command.DisplayUsage(os.Stdout)
		return nil
	}

	name := c.Args[0]
	addr := c.Args[1]
	addOrUpdateRecord("AAAA", name, addr)
	return nil
}

func cmdCNAME(c cmd.Selection) error {
	if len(c.Args) != 2 {
		c.Command.DisplayUsage(os.Stdout)
		return nil
	}

	name := c.Args[0]
	addr := c.Args[1]
	addOrUpdateRecord("CNAME", name, addr)
	return nil
}

func cmdTXT(c cmd.Selection) error {
	if len(c.Args) != 2 {
		c.Command.DisplayUsage(os.Stdout)
		return nil
	}

	name := c.Args[0]
	content := c.Args[1]
	addOrUpdateRecord("TXT", name, content)
	return nil
}

func cmdAdd(c cmd.Selection) error {
	if len(c.Args) != 3 {
		c.Command.DisplayUsage(os.Stdout)
		return nil
	}

	api := getAPI()
	if api == nil {
		return nil
	}

	zoneID := getZoneID()
	if zoneID == "" {
		return nil
	}

	recType := c.Args[0]
	name := c.Args[1]
	content := c.Args[2]

	r := cloudflare.DNSRecord{
		Type:    recType,
		Name:    name,
		Content: content,
		Proxied: false,
		TTL:     1,
	}
	_, err := api.CreateDNSRecord(zoneID, r)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return nil
	}

	fmt.Println("DNS record added.")
	return nil
}

func cmdDelete(c cmd.Selection) error {
	if len(c.Args) != 2 {
		c.Command.DisplayUsage(os.Stdout)
		return nil
	}

	api := getAPI()
	if api == nil {
		return nil
	}

	zoneID := getZoneID()
	if zoneID == "" {
		return nil
	}

	recType := c.Args[0]
	name := c.Args[1]

	if len(recType) < 1 {
		fmt.Printf("Must provide valid DNS record type.")
		return nil
	}

	recs, err := api.DNSRecords(zoneID, cloudflare.DNSRecord{Type: recType, Name: name})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return nil
	}
	if len(recs) < 1 {
		fmt.Println("No matching record(s) found.")
		return nil
	}

	for _, r := range recs {
		err := api.DeleteDNSRecord(zoneID, r.ID)
		if err != nil {
			fmt.Printf("Error deleting %s: %v\n", r.Name, err)
			continue
		}
		fmt.Printf("Deleted %s record %s.\n", r.Type, r.Name)
	}

	return nil
}

func addOrUpdateRecord(recType, name, content string) {
	api := getAPI()
	if api == nil {
		return
	}

	zoneID := getZoneID()
	if zoneID == "" {
		return
	}

	recs, err := api.DNSRecords(zoneID, cloudflare.DNSRecord{Type: recType, Name: name})
	if err == nil && len(recs) > 0 {
		r := recs[0]
		if r.Content != content {
			r.Content = content
			r.Proxied = false
			err = api.UpdateDNSRecord(zoneID, r.ID, r)
		}
	} else {
		r := cloudflare.DNSRecord{
			Type:    recType,
			Name:    name,
			Content: content,
			Proxied: false,
			TTL:     1,
		}
		_, err = api.CreateDNSRecord(zoneID, r)
	}

	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Println("DNS record updated.")
}

func readString(prompt string) (string, error) {
	fmt.Print(prompt)

	reader := bufio.NewReader(os.Stdin)
	text, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(text, "\r\n"), nil
}

func readHiddenString(prompt string) (string, error) {
	fmt.Print(prompt)

	bytes, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		return "", err
	}
	fmt.Println()

	return string(bytes), nil
}

func getAPI() *cloudflare.API {
	if activeAPI != nil {
		return activeAPI
	}

	key := os.Getenv("CLOUDFLARE_KEY")
	email := os.Getenv("CLOUDFLARE_EMAIL")

	var err error
	if email == "" {
		if interactive {
			email, _ = readString("Enter cloudflare account email: ")
		} else {
			fmt.Println("CLOUDFLARE_EMAIL not set.")
			return nil
		}
	}

	if key == "" {
		if interactive {
			key, _ = readHiddenString("Enter cloudflare API key: ")
		} else {
			fmt.Println("CLOUDFLARE_KEY not set.")
			return nil
		}
	}

	activeAPI, err = cloudflare.New(key, email)
	if err != nil {
		fmt.Printf("Error: %v", err)
		return nil
	}

	return activeAPI
}

func getZoneID() string {
	api := getAPI()
	if api == nil {
		return ""
	}

	if activeZoneID != "" {
		return activeZoneID
	}

	zoneName := os.Getenv("CLOUDFLARE_ZONE")

	var err error
	if zoneName == "" && interactive {
		zoneName, _ = readString("Enter zone name: ")
	}

	if zoneName == "" {
		fmt.Println("CLOUDFLARE_ZONE not set.")
		return ""
	}

	activeZoneID, err = api.ZoneIDByName(zoneName)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return ""
	}

	return activeZoneID
}
