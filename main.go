package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	client "github.com/pzy2000/healthreport/httpclient"
	"github.com/pzy2000/healthreport/serve"
	"github.com/pzy2000/healthreport/utils/config"
	"github.com/pzy2000/healthreport/utils/email"
	"github.com/pzy2000/healthreport/utils/systemd"
)

// build info
var (
	BuildTime       = "Not Provided."
	ProgramCommitID = "Not Provided."
	ProgramVersion  = "Not Provided."
)

const (
	mailNickName = "打卡状态推送"

	retryAfter   = 5 * time.Minute
	punchTimeout = 30 * time.Second
)

var (
	cfg     = &config.Config{}
	account = &client.Account{}

	mailConfigPath  string
	accountFilename string // 账户信息存储文件名
	logger          = log.Default()
)

func main() {
	defer logger.Print("Exit\n")
	logger.Print("Start program\n")
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
	for {
		ctx, cc := context.WithCancel(context.Background())
		exit := false
		go func() {
			switch <-c {
			case syscall.SIGHUP:
				systemd.Notify(systemd.Reloading)
				cc()
			case syscall.SIGINT, syscall.SIGTERM:
				systemd.Notify(systemd.Stopping)
				exit = true
				cc()
			}
		}()
		app(ctx, func() {
			systemd.Notify(systemd.Ready)
		})
		if exit {
			break
		}
		initApp() // load config
	}
}

func app(ctx context.Context, ready func()) {
	cfg.Show(logger)

	emailCfg, err := email.LoadConfig(mailConfigPath)
	if err == nil {
		logger.Print("Email deliver enabled\n")
	}

	logger.Print("正在验证账号密码...\n")
	err = client.LoginConfirm(ctx, account, punchTimeout)
	if err != nil {
		logger.Fatalf("验证密码失败(Err: %s)\n", err.Error())
	}
	ready()
	logger.Print("账号密码验证成功，Punch in 5 secs!\n")

	serveCfg := &serve.Config{
		Sender:      emailCfg,
		Logger:      logger,
		MaxAttempts: uint8(cfg.MaxAttempts),
		Time: serve.Time{
			Hour:     cfg.PunchTime.Hour,
			Minute:   cfg.PunchTime.Minute,
			TimeZone: time.FixedZone("CST", 8*3600), // China Standard Time Zone,
		},
		MailNickName: mailNickName,
		Timeout:      punchTimeout,
		RetryAfter:   retryAfter,
		PunchFunc:    client.Punch,
	}

	{
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-timer.C:
			break
		case <-ctx.Done():
			timer.Stop()
			return
		}
	}
	err = serveCfg.PunchServe(ctx, account)
	if err != nil && err != context.Canceled {
		logger.Fatalln(err.Error())
	}
}

func init() {
	initApp()
}

func initApp() {
	var (
		version    bool
		checkEmail bool
		save       bool
	)

	flagSet := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flagSet.BoolVar(&version, "v", false, "show version and exit")
	flagSet.BoolVar(&checkEmail, "e", false, "check email")
	flagSet.StringVar(&account.Username, "u", "", "set username")
	flagSet.StringVar(&account.Password, "p", "", "set password")
	flagSet.StringVar(&mailConfigPath, "email", "email.json", "set email config file path")
	flagSet.StringVar(&accountFilename, "account", "account.json", "set account file path(json format with keys:'username','password')")
	flagSet.BoolVar(&save, "save", false, "whether save config to file")
	cfg.SetFlag(flagSet)
	if err := flagSet.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		logger.Fatalln(err.Error())
	}

	if version {
		fmt.Printf("Program Version:        %s\n", ProgramVersion)
		fmt.Printf("Go Version:             %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
		fmt.Printf("Build Time:             %s\n", BuildTime)
		fmt.Printf("Program Commit ID:      %s\n", ProgramCommitID)
		os.Exit(0)
	}

	if checkEmail {
		cfg, err := email.LoadConfig(mailConfigPath)
		if err == nil {
			err = cfg.LoginTest()
		}

		if err != nil {
			logger.Fatalf("email check: failed, err: %s\n", err.Error())
		}
		fmt.Print("email check: pass\n")
		os.Exit(0)
	}

	fromArgs := account.Username != "" || account.Password != ""

	if !fromArgs {
		err := loadJson(account, accountFilename)
		if err != nil {
			logger.Fatalln(err.Error())
		}
	}

	if save && fromArgs {
		if err := storeJson(account, accountFilename); err != nil {
			logger.Printf("account: save to file failed(Err: %s)\n", err.Error())
		}
	}
}

func loadJson(v interface{}, name string) error {
	f, err := os.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewDecoder(f).Decode(v)
}

func storeJson(v interface{}, name string) error {
	f, err := os.OpenFile(name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "\t")
	return enc.Encode(v)
}
