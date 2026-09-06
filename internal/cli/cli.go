package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/light-speak/aitodos/internal/buildinfo"
	"github.com/light-speak/aitodos/internal/daemon"
	"github.com/light-speak/aitodos/internal/mcpserver"
	"github.com/light-speak/aitodos/internal/project"
	"github.com/light-speak/aitodos/internal/projectstate"
	"github.com/light-speak/aitodos/internal/runner"
	"github.com/light-speak/aitodos/internal/storage"
)

// Run 执行 ATS CLI 并返回进程退出码。
func Run(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}
	var err error
	switch args[0] {
	case "init":
		err = runInit(ctx, args[1:], stdout, stderr)
	case "start":
		err = runStart(ctx, args[1:], stdout, stderr)
	case "status":
		err = runStatus(ctx, args[1:], stdout, stderr)
	case "stop":
		err = runStop(ctx, args[1:], stdout, stderr)
	case "open":
		err = runOpen(ctx, args[1:], stdout, stderr)
	case "mcp":
		err = runMCP(ctx, args[1:], os.Stdin, stdout)
	case "backup", "export":
		err = runBackup(ctx, args[1:], stdout, stderr)
	case "restore":
		err = runRestore(ctx, args[1:], stdout, stderr)
	case "doctor":
		err = runDoctor(ctx, args[1:], stdout, stderr)
	case "runner":
		err = runRunner(ctx, args[1:], stderr)
	case "version", "--version":
		fmt.Fprintln(stdout, buildinfo.String())
		return 0
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "未知命令: %s\n", args[0])
		printUsage(stderr)
		return 2
	}
	if err != nil {
		fmt.Fprintf(stderr, "错误: %v\n", err)
		return 1
	}
	return 0
}

func runBackup(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	flags := flag.NewFlagSet("backup", flag.ContinueOnError)
	flags.SetOutput(stderr)
	output := flags.String("output", "", "备份 ZIP 输出路径")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("ats backup 不接受位置参数")
	}
	currentProject, err := loadCurrentProject(ctx)
	if err != nil {
		return err
	}
	if _, running := daemon.Status(ctx, currentProject); running {
		return errors.New("备份前请在另一个终端执行 ats stop，确保没有数据库写入")
	}
	selected := strings.TrimSpace(*output)
	if selected == "" {
		selected = filepath.Join(currentProject.Paths.ATSRoot, "backups", "aitodos-"+time.Now().UTC().Format("20060102T150405Z")+".zip")
	}
	manifest, err := projectstate.Backup(ctx, currentProject, selected)
	if err != nil {
		return err
	}
	absolute, _ := filepath.Abs(selected)
	fmt.Fprintf(stdout, "备份完成: %s\n文件数: %d\n", absolute, len(manifest.Files))
	return nil
}

func runRestore(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	flags := flag.NewFlagSet("restore", flag.ContinueOnError)
	flags.SetOutput(stderr)
	input := flags.String("input", "", "备份 ZIP 路径")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*input) == "" {
		return errors.New("ats restore 需要 --input BACKUP.zip")
	}
	currentProject, err := loadCurrentProject(ctx)
	if err != nil {
		return err
	}
	if _, running := daemon.Status(ctx, currentProject); running {
		return errors.New("恢复前必须停止当前项目 Daemon")
	}
	manifest, err := projectstate.Restore(ctx, currentProject, *input)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "恢复完成: %s\n备份时间: %s\n", currentProject.Root, manifest.CreatedAt.Format(time.RFC3339))
	return nil
}

func runDoctor(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(stderr)
	jsonOutput := flags.Bool("json", false, "以 JSON 输出完整性结果")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("ats doctor 不接受位置参数")
	}
	currentProject, err := loadCurrentProject(ctx)
	if err != nil {
		return err
	}
	report, err := projectstate.CheckIntegrity(ctx, currentProject)
	if err != nil {
		return err
	}
	if *jsonOutput {
		output := doctorJSONReport{
			SchemaVersion:     1,
			ProjectInstanceID: currentProject.InstanceID,
			OK:                report.OK,
			Problems:          report.Problems,
		}
		if err := json.NewEncoder(stdout).Encode(output); err != nil {
			return fmt.Errorf("输出完整性检查 JSON: %w", err)
		}
	}
	if !report.OK {
		return fmt.Errorf("项目完整性检查失败: %s", strings.Join(report.Problems, "; "))
	}
	if !*jsonOutput {
		fmt.Fprintln(stdout, "项目完整性检查通过")
	}
	return nil
}

type doctorJSONReport struct {
	SchemaVersion     int      `json:"schema_version"`
	ProjectInstanceID string   `json:"project_instance_id"`
	OK                bool     `json:"ok"`
	Problems          []string `json:"problems"`
}

func runMCP(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := flag.NewFlagSet("mcp", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("ats mcp 不接受位置参数")
	}
	currentProject, err := loadCurrentProject(ctx)
	if err != nil {
		return err
	}
	database, err := storage.OpenExisting(ctx, currentProject.Paths.Database)
	if err != nil {
		return fmt.Errorf("open MCP project database: %w", err)
	}
	defer database.Close()
	return mcpserver.New(database).Serve(ctx, stdin, stdout)
}

func runRunner(ctx context.Context, args []string, stderr io.Writer) error {
	flags := flag.NewFlagSet("runner", flag.ContinueOnError)
	flags.SetOutput(stderr)
	projectRoot := flags.String("project", "", "项目根目录")
	runID := flags.String("run", "", "Run ID")
	runNonce := flags.String("nonce", "", "Run nonce")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *projectRoot == "" || *runID == "" || *runNonce == "" {
		return errors.New("runner 需要 --project、--run 和 --nonce")
	}
	claimToken, err := readRunnerClaimToken()
	generationText := os.Getenv("ATS_LEASE_GENERATION")
	if err != nil || generationText == "" {
		return errors.New("runner 缺少 Claim 环境")
	}
	generation, err := strconv.ParseInt(generationText, 10, 64)
	if err != nil || generation < 1 {
		return errors.New("runner Lease Generation 无效")
	}
	currentProject, err := project.Load(ctx, *projectRoot)
	if err != nil {
		return err
	}
	return runner.Execute(ctx, currentProject, *runID, claimToken, generation, *runNonce)
}

func readRunnerClaimToken() (string, error) {
	fdText := os.Getenv("ATS_CLAIM_FD")
	fd, err := strconv.Atoi(fdText)
	if err != nil || fd < 3 {
		return "", errors.New("runner Claim FD 无效")
	}
	file := os.NewFile(uintptr(fd), "ats-claim")
	if file == nil {
		return "", errors.New("runner Claim FD 不存在")
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, 1025))
	if err != nil || len(content) > 1024 {
		return "", errors.New("读取 Runner Claim 失败")
	}
	token := strings.TrimSpace(string(content))
	if token == "" {
		return "", errors.New("Runner Claim 为空")
	}
	return token, nil
}

func runInit(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("ats init 不接受位置参数")
	}
	directory, err := os.Getwd()
	if err != nil {
		return err
	}
	currentProject, created, err := project.Initialize(ctx, directory)
	if err != nil {
		return err
	}
	if created {
		fmt.Fprintf(stdout, "已初始化 ATS 项目: %s\n", currentProject.Root)
	} else {
		fmt.Fprintf(stdout, "ATS 项目已经初始化: %s\n", currentProject.Root)
	}
	fmt.Fprintf(stdout, "数据库: %s\n", currentProject.Paths.Database)
	fmt.Fprintf(stdout, "项目并发: %d\n", currentProject.Local.Agent.MaxWorkers)
	return nil
}

func runStart(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	flags := flag.NewFlagSet("start", flag.ContinueOnError)
	flags.SetOutput(stderr)
	port := flags.Int("port", -1, "监听端口；默认读取 .ats/local.toml，0 表示随机端口")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("ats start 不接受位置参数")
	}
	currentProject, err := loadCurrentProject(ctx)
	if err != nil {
		return err
	}
	selectedPort := currentProject.Local.Server.Port
	if *port != -1 {
		selectedPort = *port
	}
	if selectedPort < 0 || selectedPort > 65535 {
		return errors.New("--port 必须为 0 到 65535；0 表示使用随机端口")
	}
	return daemon.Serve(ctx, currentProject, "foreground", selectedPort, func(metadata daemon.Metadata) {
		printStarted(stdout, currentProject, metadata)
	})
}

func runStatus(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	flags := flag.NewFlagSet("status", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("ats status 不接受位置参数")
	}
	currentProject, err := loadCurrentProject(ctx)
	if err != nil {
		return err
	}
	metadata, running := daemon.Status(ctx, currentProject)
	if !running {
		fmt.Fprintf(stdout, "ATS 项目未运行: %s\n", currentProject.Root)
		return nil
	}
	fmt.Fprintf(stdout, "ATS 项目前台正在运行\n项目: %s\nPID: %d\nURL: %s\n",
		currentProject.Root,
		metadata.PID,
		metadata.URL,
	)
	return nil
}

func runStop(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	flags := flag.NewFlagSet("stop", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("ats stop 不接受位置参数")
	}
	currentProject, err := loadCurrentProject(ctx)
	if err != nil {
		return err
	}
	stopped, err := daemon.Stop(ctx, currentProject)
	if err != nil {
		return err
	}
	if !stopped {
		fmt.Fprintln(stdout, "ATS 项目未运行")
		return nil
	}
	fmt.Fprintln(stdout, "ATS 项目运行进程已停止")
	return nil
}

func runOpen(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	flags := flag.NewFlagSet("open", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("ats open 不接受位置参数")
	}
	currentProject, err := loadCurrentProject(ctx)
	if err != nil {
		return err
	}
	metadata, running := daemon.Status(ctx, currentProject)
	if !running {
		return errors.New("ATS 项目未运行，请先执行 ats start 并保持该终端开启")
	}
	if err := daemon.OpenBrowser(metadata.URL); err != nil {
		return err
	}
	fmt.Fprintln(stdout, metadata.URL)
	return nil
}

func loadCurrentProject(ctx context.Context) (*project.Project, error) {
	directory, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return project.Load(ctx, directory)
}

func printStarted(
	output io.Writer,
	currentProject *project.Project,
	metadata daemon.Metadata,
) {
	fmt.Fprintf(output, "ATS 项目前台已启动\n项目: %s\nPID: %d\nURL: %s\n保持当前终端开启，按 Ctrl+C 停止\n",
		currentProject.Root,
		metadata.PID,
		metadata.URL,
	)
}

func printUsage(output io.Writer) {
	fmt.Fprintln(output, `用法: ats <command>

命令:
  init                    初始化当前 Git 项目
  start [--port PORT]     前台启动当前项目，不会自动打开浏览器
  status                  查看当前项目运行状态
  open                    打开当前项目页面
  stop                    停止当前项目运行进程`)
	fmt.Fprintln(output, "  mcp                     启动当前项目只读 MCP stdio Server")
	fmt.Fprintln(output, "  backup [--output PATH]  备份项目事实数据（export 同义）")
	fmt.Fprintln(output, "  restore --input PATH    校验并恢复项目事实数据")
	fmt.Fprintln(output, "  doctor [--json]         检查数据库、外键和 Artifact 完整性")
	fmt.Fprintln(output, "  version                 显示二进制版本、Commit 和构建时间")
}
