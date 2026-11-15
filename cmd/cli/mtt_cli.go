package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/Recruit-CSIRT/macOSTriageTool/pkg/log"
	"github.com/Recruit-CSIRT/macOSTriageTool/pkg/triage"
	"github.com/Recruit-CSIRT/macOSTriageTool/pkg/conf"
	"github.com/Recruit-CSIRT/macOSTriageTool/pkg/utils"
)

var config conf.Config

func init() {

	flag.StringVar(&config.RootPath, "root", "/", "set the evidence root path.")
	flag.StringVar(&config.OutputRootPath, "output", ".", "set the output path.")

	flag.IntVar(&config.SelectedPresetNum, "preset", 0, "select forensics type: 0(AllList), 1(Malware), 2(Fraud), 3(macripper), 4(only custom). (default: 0)")
	flag.StringVar(&config.UserDefinedFileListName, "file", "", "set user custom file list path.")

	flag.BoolVar(&config.IsEnabledToGetProfiler, "i", false, "get the system information. (default: false)")
	flag.BoolVar(&config.IsEnabledToCalcHash, "c", false, "calc the file hash. (default: false)")
	flag.BoolVar(&config.IsEnabledToGetStatInfo, "s", false, "get the stat info. (default: false)")
	flag.BoolVar(&config.IsEnabledToSaveIntoDmg, "d", false, "save files into a dmg. (default: false)")
	flag.BoolVar(&config.IsEnabledToWriteBodyfile, "bodyfile", false, "write a TSK bodyfile of ALL files under the root (-exclude is NOT applied). (default: false)")
	flag.IntVar(&config.Jobs, "j", runtime.NumCPU(), "number of parallel copy workers.")
	flag.Func("exclude", "guest path glob to exclude from copy and stat info (not from -bodyfile), e.g. '/Users/*/Documents'. repeatable.", func(v string) error {
		config.Excludes = append(config.Excludes, v)
		return nil
	})

}

func main() {

	flag.Parse()

	if err := utils.CheckOutputOutsideRoot(config.RootPath, config.OutputRootPath); err != nil {
		fmt.Println(err.Error())
		return
	}
	if _, err := utils.NewExcluder(config.Excludes); err != nil {
		fmt.Println(err.Error())
		return
	}

	// Refuse a used output dir: stale files from an earlier run (e.g. without
	// -exclude) would otherwise survive into the new collection.
	if entries, err := os.ReadDir(filepath.Join(config.OutputRootPath, conf.OutputDirName)); err == nil && len(entries) > 0 {
		fmt.Println("[-] Output dir is not empty: " + filepath.Join(config.OutputRootPath, conf.OutputDirName))
		return
	}

	err := config.Setting()
	if err != nil {
		fmt.Println(err.Error())
		return
	}

	err = log.Init(config.OutputRootPath)
	if err != nil {
		fmt.Println(err.Error())
		return
	}

	err = triage.Triage(&config)
	if err != nil {
		log.ToolLogger.Error(err.Error())
		return
	}

}
