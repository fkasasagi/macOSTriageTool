	package triage

	import (
		"bytes"
		"fmt"
		"os"
		"os/exec"
		"path/filepath"
		"strings"
		"sync"

		"github.com/Recruit-CSIRT/macOSTriageTool/pkg/conf"
		"github.com/Recruit-CSIRT/macOSTriageTool/pkg/log"
		"github.com/Recruit-CSIRT/macOSTriageTool/pkg/utils"
	)

func Triage(config *conf.Config) error {

	log.ToolLogger.Info("[+] Tool Start")

	triageList := utils.TriageFileList{}
	err := triageList.New(config)
	if err != nil {
		return err
	}

	fsnfl, err := os.OpenFile(filepath.Join(config.OutputRootPath, conf.ScheduledTriageFileListName), os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		log.ToolLogger.Error("[-] Failed to open the file: " + err.Error())
		return err
	}
	defer fsnfl.Close()

	_, err = fsnfl.WriteString(strings.Join(triageList.NormalizedFileList, "\n"))
	if err != nil {
		log.ToolLogger.Error("[-] Failed to write the list: " + err.Error())
		return err
	}


	if config.IsEnabledToCalcHash || config.IsEnabledToGetStatInfo || config.IsEnabledToSaveIntoDmg {
		log.ToolLogger.Info("[+] Start processing the options")

		utils.WriteColumn()

		for _, f := range triageList.NormalizedFileList {
			err := processFileOptions(&f, config)
			if err != nil {
				log.FileInfoErrorLogger.Error(err.Error())
			}
		}

		log.ToolLogger.Info("[+] Finish processing the options")
	}

	// create dir or dmg for file
	log.ToolLogger.Info("[+] Creating the output dir or dmg")
	if config.IsEnabledToSaveIntoDmg {
		err = config.CreateAndMountDmg()
		defer config.UnmountDmg()
	} else {
		err = config.MakeOutputDir()
	}
	if err != nil {
		return nil
	}


	log.ToolLogger.Info("[+] Start copying files")
	copyAll(triageList.NormalizedFileList, config)
	log.ToolLogger.Info("[+] Finish copying files")

	if config.IsEnabledToWriteBodyfile {
		log.ToolLogger.Info("[+] Start writing bodyfile of all files under the root")
		if err := writeBodyfile(config); err != nil {
			log.ToolLogger.Error("[-] Failed to write bodyfile: " + err.Error())
		}
		log.ToolLogger.Info("[+] Finish writing bodyfile")
	}

	if config.IsEnabledToGetProfiler {
		log.ToolLogger.Info("[+] Start getting system profiler")
		err := getSystemProfiler(config)
		if err != nil {
			log.ToolLogger.Info("[-] Failed to get system profiler")
		}
		log.ToolLogger.Info("[+] Finish getting system profiler")
	}

	log.ToolLogger.Info("[+] Tool Finish")

	return nil
}

// copyAll copies the (non-overlapping) guest paths with config.Jobs parallel ditto workers.
func copyAll(guests []string, config *conf.Config) {
	jobs := config.Jobs
	if jobs < 1 {
		jobs = 1
	}
	// Create every parent directory up front: parallel ditto runs race on
	// creating a shared parent ("File exists") and drop files silently.
	for _, g := range guests {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(config.OutputDirPath, g)), 0o755); err != nil {
			log.FileCopyLogger.Error("[-] Failed to create parent of: " + g + ": " + err.Error())
		}
	}
	ch := make(chan string)
	var wg sync.WaitGroup
	for i := 0; i < jobs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for g := range ch {
				copyFile(g, config)
			}
		}()
	}
	for _, g := range guests {
		ch <- g
	}
	close(ch)
	wg.Wait()
}

func copyFile(guest string, config *conf.Config) {
	src := filepath.Join(config.RootPath, guest)
	dst := filepath.Join(config.OutputDirPath, guest)
	fileFrom := &guest

	// ditto follows a symlink given as the source; copy the link itself so
	// nothing outside the root (or in an excluded tree) is ever read.
	if info, err := os.Lstat(src); err == nil && info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err == nil {
			err = os.Symlink(target, dst)
		}
		if err != nil {
			log.FileCopyLogger.Error("[-] Failed to copy symlink: " + guest + ": " + err.Error())
		} else {
			log.FileCopyLogger.Info("[+] Copied symlink: " + guest)
		}
		return
	}

	// ditto --rsrc --extattr --qtn --acl src dst  (keeps quarantine/xattrs, ACLs, times)
	cmd := exec.Command(conf.CmdDitto, "--rsrc", "--extattr", "--qtn", "--acl", src, dst)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		log.FileCopyLogger.Error("[-] Failed to copy: " + *fileFrom)
		log.FileCopyLogger.Error("[-] Message: " + strings.TrimRight(stderr.String(), "\n"))
	} else {
		log.FileCopyLogger.Info("[+] Copied: " + *fileFrom)
	}

}

func processFileOptions(guest *string, config *conf.Config) error {

	root := filepath.Join(config.RootPath, *guest)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {

		if err != nil {
			log.FileInfoErrorLogger.Error("[-] Prevent panic by handling failure accessing a path. Path: " + path + ", Error: " + err.Error())
			return err
		}

		// stat
		rel, _ := filepath.Rel(config.RootPath, path)
		customFileInfo := utils.NewFileInfo(path, "/"+filepath.ToSlash(rel))
		if (config.IsEnabledToGetStatInfo || config.IsEnabledToSaveIntoDmg) && info != nil {
			customFileInfo.SetStat(&info)
		}

		// hash
		if config.IsEnabledToCalcHash && info.Mode().IsRegular() {
			_ = customFileInfo.SetHash()
		}

		if config.IsEnabledToCalcHash || config.IsEnabledToGetStatInfo {
			customFileInfo.WriteLogger()
		}

		// calc size of all files
		if config.IsEnabledToSaveIntoDmg {
			config.DmgSize += int64(info.Size())
		}

		return nil
	})
	if err != nil {
		log.FileInfoErrorLogger.Error("[-] Error walking the path" + err.Error())
		return err
	}

	return nil
}

func writeBodyfile(config *conf.Config) error {
	ex, err := utils.NewExcluder(config.Excludes)
	if err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(config.OutputRootPath, conf.BodyfileName))
	if err != nil {
		return err
	}
	defer f.Close()
	n, err := utils.WriteBodyfile(f, config.RootPath, ex)
	log.ToolLogger.Info(fmt.Sprintf("[+] bodyfile entries: %d", n))
	return err
}

func getSystemProfiler(config *conf.Config) error {

	cmd := exec.Command(conf.CmdSystemProfiler)

	output, err := cmd.Output()
		if err != nil {
		return err
	}

	file, err := os.Create(filepath.Join(config.OutputRootPath, conf.SystemProfilerFileName))
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = fmt.Fprintln(file, string(output))
	if err != nil {
		return err
	}

	return nil
}
