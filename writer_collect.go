package nya

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// collectDirectoryPaths walks root and returns non-root directory paths and
// all non-directory member paths (files, symlinks, devices, …).
func collectDirectoryPaths(root string) (allPaths []string, err error) {
	err = filepath.Walk(root, func(p string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if p != root {
			allPaths = append(allPaths, p)
		}
		return nil
	})
	return allPaths, err
}

func splitDirectoryPaths(basePath string, allPaths []string, nw *Writer) []string {
	var files []string
	for _, p := range allPaths {
		fi, err := os.Lstat(p)
		if err != nil {
			continue
		}
		if fi.IsDir() {
			relPath, _ := filepath.Rel(basePath, p)
			nw.addDirEntry(relPath, fi)
			continue
		}
		files = append(files, p)
	}
	return files
}

func (nw *Writer) addCollectedFiles(files []string) error {
	if nw.solid {
		files = sortSolidFilePaths(files)
		nw.solidSourceFiles = append(nw.solidSourceFiles, files...)
		for _, f := range files {
			fi, err := os.Stat(f)
			if err != nil {
				return err
			}
			if err := nw.addFile(f, fi); err != nil {
				return err
			}
		}
		return nil
	}

	workers := runtime.NumCPU()
	if nw.workers > 0 {
		workers = nw.workers
	}
	if workers > 4 {
		workers = 4
	}
	sem := make(chan struct{}, workers)
	var mu sync.Mutex
	var wg sync.WaitGroup
	var firstErr error
	for _, f := range files {
		wg.Add(1)
		sem <- struct{}{}
		go func(fp string) {
			defer wg.Done()
			defer func() { <-sem }()
			fi, err := os.Stat(fp)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}
			mu.Lock()
			if firstErr == nil {
				firstErr = nw.addFile(fp, fi)
			}
			mu.Unlock()
		}(f)
	}
	wg.Wait()
	return firstErr
}
