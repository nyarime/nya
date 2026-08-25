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
	nyaProgID     = "Nyarime.NYA"
	nyaExtKey     = `.nya`
	nyaFileDesc   = "Nyarime Archive"
	nyaShellOpen  = `open`
)

func cmdAssociate(args []string) error {
	uninstall := false
	for _, a := range args {
		switch a {
		case "-uninstall", "--uninstall", "uninstall":
			uninstall = true
		case "-h", "--help":
			fmt.Print(`Usage:
  nya associate              register .nya → double-click extracts to .<name>\
  nya associate -uninstall   remove the file association (current user)
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
	// Quote path for registry command line.
	cmd := fmt.Sprintf(`"%s" open "%%1"`, exe)

	classes, err := registry.OpenKey(registry.CURRENT_USER, `Software\Classes`, registry.SET_VALUE|registry.CREATE_SUB_KEY)
	if err != nil {
		return fmt.Errorf("associate: open Classes: %w", err)
	}
	defer classes.Close()

	ext, _, err := registry.CreateKey(classes, nyaExtKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	if err := ext.SetStringValue("", nyaProgID); err != nil {
		ext.Close()
		return err
	}
	ext.Close()

	prog, _, err := registry.CreateKey(classes, nyaProgID, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer prog.Close()
	if err := prog.SetStringValue("", nyaFileDesc); err != nil {
		return err
	}

	shell, _, err := registry.CreateKey(prog, `shell\`+nyaShellOpen+`\command`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer shell.Close()
	if err := shell.SetStringValue("", cmd); err != nil {
		return err
	}

	fmt.Printf("Associated %s with:\n  %s\nDouble-click a .nya file to extract into .<basename>\\\n", nyaExtKey, cmd)
	fmt.Println("(Current user only. Run: nya associate -uninstall  to remove.)")
	return nil
}

func windowsUnassociate() error {
	classes, err := registry.OpenKey(registry.CURRENT_USER, `Software\Classes`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer classes.Close()

	// Only remove if we still own the ProgID pointer.
	ext, err := registry.OpenKey(classes, nyaExtKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err == nil {
		cur, _, _ := ext.GetStringValue("")
		ext.Close()
		if strings.EqualFold(cur, nyaProgID) {
			_ = registry.DeleteKey(classes, nyaExtKey)
		}
	}
	_ = deleteTree(classes, nyaProgID)
	fmt.Println("Removed .nya file association (if it pointed at Nyarime.NYA).")
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
