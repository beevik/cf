// Copyright 2018 Brett Vickers.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The cf tool allows you to view and manipulate DNS records stored in
// your Cloudflare account.
package main

import (
	"bufio"
	"context"
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
	interactive          bool
	activeAPI            *cloudflare.API
	activeZoneIdentifier *cloudflare.ResourceContainer
	cmds                 *cmd.Tree
)

func init() {
	root := cmd.NewTree(cmd.TreeDescriptor{
		Name: "Primary",
	})

	root.AddCommand(cmd.CommandDescriptor{
		Name:        "help",
		Description: "Display help for a command.",
		Usage:       "help [<command>]",
		Data:        cmdHelp,
	})
	root.AddCommand(cmd.CommandDescriptor{
		Name:        "list",
		Brief:       "List all DNS records",
		Description: "List all DNS records in the currently active zone.",
		Usage:       "list [<type>]",
		Data:        cmdListDomains,
	})
	root.AddCommand(cmd.CommandDescriptor{
		Name:  "ip4",
		Brief: "Add or modify an IPv4 Address (type A) record",
		Description: "Add or modify an IPv4 address (type A) DNS record " +
			"in the currently active zone.",
		Usage: "ip4 <name> <address>",
		Data:  cmdIP4,
	})
	root.AddCommand(cmd.CommandDescriptor{
		Name:  "ip6",
		Brief: "Add or modify an IPv6 Address (type AAAA) record",
		Description: "Add or modify an IPv6 address (type AAAA) DNS record " +
			"in the currently active zone.",
		Usage: "ip6 <name> <address>",
		Data:  cmdIP6,
	})
	root.AddCommand(cmd.CommandDescriptor{
		Name:  "cname",
		Brief: "Add or modify a CNAME record",
		Description: "Add or modify a CNAME DNS record " +
			"in the currently active zone.",
		Usage: "cname <name> <address>",
		Data:  cmdCNAME,
	})
	root.AddCommand(cmd.CommandDescriptor{
		Name:  "txt",
		Brief: "Add or modify a text (type TXT) record",
		Description: "Add or modify a text (type TXT) DNS record " +
			"in the currently active zone.",
		Usage: "txt <name> <address>",
		Data:  cmdTXT,
	})
	root.AddCommand(cmd.CommandDescriptor{
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
	root.AddCommand(cmd.CommandDescriptor{
		Name:  "delete",
		Brief: "Delete DNS record(s)",
		Description: "Delete all DNS records matching the requested type " +
			"and name in the currently active zone. The type must be one " +
			"of the allowed DNS record types (A, AAAA, CNAME, etc.).",
		Usage: "delete <type> <name>",
		Data:  cmdDelete,
	})
	root.AddCommand(cmd.CommandDescriptor{
		Name:        "zone",
		Brief:       "Set active zone",
		Description: "Set the active zone used by all future commands.",
		Usage:       "zone <name>",
		Data:        cmdSetZone,
	})
	root.AddCommand(cmd.CommandDescriptor{
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
	var args []string
	var n cmd.Node
	if line != "" {
		n, args, err = cmds.Lookup(line)
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

	if n == nil {
		return nil
	}
	if c, ok := n.(*cmd.Command); ok {
		handler := c.Data.(func(cmd *cmd.Command, args []string) error)
		return handler(c, args)
	}
	return nil
}

func cmdQuit(c *cmd.Command, args []string) error {
	return errors.New("exiting program")
}

func cmdHelp(c *cmd.Command, args []string) error {
	if len(args) == 0 {
		c.Parent().DisplayHelp(os.Stdout)
	} else {
		n, _, err := cmds.Lookup(args[0])
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
		if cc, ok := n.(*cmd.Command); ok {
			cc.DisplayHelp(os.Stdout)
		}
	}
	return nil
}

func cmdSetZone(c *cmd.Command, args []string) error {
	if len(args) < 1 {
		c.DisplayUsage(os.Stdout)
		return nil
	}

	api := getAPI()
	if api == nil {
		return nil
	}

	zoneID, err := api.ZoneIDByName(args[0])
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return nil
	}

	activeZoneIdentifier = cloudflare.ZoneIdentifier(zoneID)
	fmt.Printf("Active zone set to %v.\n", args[0])
	return nil
}

func cmdListDomains(c *cmd.Command, args []string) error {
	zoneID := getZoneIdentifier()
	if zoneID == nil {
		return nil
	}

	api := getAPI()
	if api == nil {
		return nil
	}

	recType := ""
	if len(args) > 0 {
		recType = strings.ToUpper(args[0])
	}

	params := cloudflare.ListDNSRecordsParams{
		Type: recType,
	}
	recs, _, err := api.ListDNSRecords(context.Background(), zoneID, params)
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

func cmdIP4(c *cmd.Command, args []string) error {
	if len(args) != 2 {
		c.DisplayUsage(os.Stdout)
		return nil
	}

	name := args[0]
	addr := args[1]
	addOrUpdateRecord("A", name, addr)
	return nil
}

func cmdIP6(c *cmd.Command, args []string) error {
	if len(args) != 2 {
		c.DisplayUsage(os.Stdout)
		return nil
	}

	name := args[0]
	addr := args[1]
	addOrUpdateRecord("AAAA", name, addr)
	return nil
}

func cmdCNAME(c *cmd.Command, args []string) error {
	if len(args) != 2 {
		c.DisplayUsage(os.Stdout)
		return nil
	}

	name := args[0]
	addr := args[1]
	addOrUpdateRecord("CNAME", name, addr)
	return nil
}

func cmdTXT(c *cmd.Command, args []string) error {
	if len(args) != 2 {
		c.DisplayUsage(os.Stdout)
		return nil
	}

	name := args[0]
	content := args[1]
	addOrUpdateRecord("TXT", name, content)
	return nil
}

func cmdAdd(c *cmd.Command, args []string) error {
	if len(args) != 3 {
		c.DisplayUsage(os.Stdout)
		return nil
	}

	api := getAPI()
	if api == nil {
		return nil
	}

	zoneID := getZoneIdentifier()
	if zoneID == nil {
		return nil
	}

	recType := args[0]
	name := args[1]
	content := args[2]

	params := cloudflare.CreateDNSRecordParams{
		Type:    recType,
		Name:    name,
		Content: content,
		TTL:     1,
	}
	_, err := api.CreateDNSRecord(context.Background(), zoneID, params)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return nil
	}

	fmt.Println("DNS record added.")
	return nil
}

func cmdDelete(c *cmd.Command, args []string) error {
	if len(args) != 2 {
		c.DisplayUsage(os.Stdout)
		return nil
	}

	api := getAPI()
	if api == nil {
		return nil
	}

	zoneID := getZoneIdentifier()
	if zoneID == nil {
		return nil
	}

	recType := args[0]
	name := args[1]

	if len(recType) < 1 {
		fmt.Printf("Must provide valid DNS record type.")
		return nil
	}

	params := cloudflare.ListDNSRecordsParams{
		Type: recType,
		Name: name,
	}
	recs, _, err := api.ListDNSRecords(context.Background(), zoneID, params)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return nil
	}
	if len(recs) < 1 {
		fmt.Println("No matching record(s) found.")
		return nil
	}

	for _, r := range recs {
		err := api.DeleteDNSRecord(context.Background(), zoneID, r.ID)
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

	zoneID := getZoneIdentifier()
	if zoneID == nil {
		return
	}

	zoneIdentifier := zoneID
	params := cloudflare.ListDNSRecordsParams{
		Type: recType,
		Name: name,
	}
	recs, _, err := api.ListDNSRecords(context.Background(), zoneIdentifier, params)
	if err == nil && len(recs) > 0 {
		r := recs[0]
		if r.Content != content {
			params := cloudflare.UpdateDNSRecordParams{
				Type:    r.Type,
				Name:    name,
				Content: content,
				ID:      r.ID,
				TTL:     r.TTL,
			}
			_, err = api.UpdateDNSRecord(context.Background(), zoneIdentifier, params)
		}
	} else {
		params := cloudflare.CreateDNSRecordParams{
			Type:      recType,
			Name:      name,
			Content:   content,
			TTL:       1,
			Proxiable: false,
		}
		_, err = api.CreateDNSRecord(context.Background(), zoneIdentifier, params)
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

	var err error
	email := os.Getenv("CLOUDFLARE_EMAIL")
	if email == "" {
		if interactive {
			email, _ = readString("Enter cloudflare account email: ")
		} else {
			fmt.Println("CLOUDFLARE_EMAIL not set.")
			return nil
		}
	}

	key := os.Getenv("CLOUDFLARE_KEY")
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

func getZoneIdentifier() *cloudflare.ResourceContainer {
	if activeZoneIdentifier != nil {
		return activeZoneIdentifier
	}

	api := getAPI()
	if api == nil {
		return nil
	}

	var err error
	zoneName := os.Getenv("CLOUDFLARE_ZONE")
	if zoneName == "" && interactive {
		zoneName, _ = readString("Enter zone name: ")
	}
	if zoneName == "" {
		fmt.Println("CLOUDFLARE_ZONE not set.")
		return nil
	}

	zoneID, err := api.ZoneIDByName(zoneName)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return nil
	}

	activeZoneIdentifier = cloudflare.ZoneIdentifier(zoneID)
	return activeZoneIdentifier
}
