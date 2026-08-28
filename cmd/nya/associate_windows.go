//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	nyaProgID      = "Nyarime.NYA"
	nyamProgID     = "Nyarime.NYAM"
	nyaExtKey      = `.nya`
	nyamExtKey     = `.nyam`
	nyaFileDesc    = "Nyarime Archive"
	nyamFileDesc   = "NYA Download Manifest"
	nyaShellOpen   = `open`
)

func cmdAssociate(args []string) error {
	uninstall := false
	for _, a := range args {
		switch a {
		case "-uninstall", "--uninstall", "uninstall":
			uninstall = true
		case "-h", "--help":
			fmt.Print(`Usage:
  nya associate              register .nya → nya open; .nyam → nya get
  nya associate -uninstall   remove file associations (current user)
`)
			return nil
		}
	}
	if uninstall {
		return windowsUnassociate()
	}
	return windowsAssociate()
}

func windowsAssociate() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return err
	}
	openCmd := fmt.Sprintf(`"%s" open "%%1"`, exe)
	getCmd := fmt.Sprintf(`"%s" get "%%1"`, exe)

	classes, err := registry.OpenKey(registry.CURRENT_USER, `Software\Classes`, registry.SET_VALUE|registry.CREATE_SUB_KEY)
	if err != nil {
		return fmt.Errorf("associate: open Classes: %w", err)
	}
	defer classes.Close()

	if err := windowsRegisterExt(classes, nyaExtKey, nyaProgID, nyaFileDesc, openCmd); err != nil {
		return err
	}
	if err := windowsRegisterExt(classes, nyamExtKey, nyamProgID, nyamFileDesc, getCmd); err != nil {
		return err
	}

	fmt.Printf("Associated %s → nya open (extract beside archive)\n", nyaExtKey)
	fmt.Printf("Associated %s → nya get (download .nya; no auto-extract)\n", nyamExtKey)
	fmt.Println("(Current user only. Run: nya associate -uninstall  to remove.)")
	return nil
}

func windowsRegisterExt(classes registry.Key, extKey, progID, desc, cmd string) error {
	ext, _, err := registry.CreateKey(classes, extKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	if err := ext.SetStringValue("", progID); err != nil {
		ext.Close()
		return err
	}
	ext.Close()

	prog, _, err := registry.CreateKey(classes, progID, registry.SET_VALUE)
	if err != nil {
		return err
	}
	if err := prog.SetStringValue("", desc); err != nil {
		prog.Close()
		return err
	}
	prog.Close()

	shell, _, err := registry.CreateKey(prog, `shell\`+nyaShellOpen+`\command`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	if err := shell.SetStringValue("", cmd); err != nil {
		shell.Close()
		return err
	}
	shell.Close()
	return nil
}

func windowsUnassociate() error {
	classes, err := registry.OpenKey(registry.CURRENT_USER, `Software\Classes`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer classes.Close()

	for extKey, progID := range map[string]string{nyaExtKey: nyaProgID, nyamExtKey: nyamProgID} {
		ext, err := registry.OpenKey(classes, extKey, registry.QUERY_VALUE|registry.SET_VALUE)
		if err == nil {
			cur, _, _ := ext.GetStringValue("")
			ext.Close()
			if strings.EqualFold(cur, progID) {
				_ = registry.DeleteKey(classes, extKey)
			}
		}
		_ = deleteTree(classes, progID)
	}
	fmt.Println("Removed .nya / .nyam file associations (if they pointed at Nyarime.*).")
	return nil
}

func deleteTree(parent registry.Key, path string) error {
	k, err := registry.OpenKey(parent, path, registry.ENUMERATE_SUB_KEYS|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	for {
		names, err := k.ReadSubKeyNames(-1)
		if err != nil || len(names) == 0 {
			break
		}
		for _, name := range names {
			_ = deleteTree(k, name)
		}
	}
	k.Close()
	return registry.DeleteKey(parent, path)
}
