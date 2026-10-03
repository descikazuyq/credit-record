package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runCLI(t *testing.T, file string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errb bytes.Buffer
	full := append([]string{"-f", file}, args...)
	code = run(full, &out, &errb)
	return out.String(), errb.String(), code
}

func TestCLIEndToEnd(t *testing.T) {
	file := filepath.Join(t.TempDir(), "records.json")

	// 文件不存在时从空记录开始。
	out, _, code := runCLI(t, file, "student", "s1")
	if code != 0 || !strings.Contains(out, "已登记学生") {
		t.Fatalf("登记学生失败 code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "从空记录开始") {
		t.Fatalf("首次创建应提示从空记录开始，out=%q", out)
	}

	// 课程与要求。
	if _, _, code := runCLI(t, file, "course", "c1", "数学", "4"); code != 0 {
		t.Fatal("登记课程失败")
	}
	if _, _, code := runCLI(t, file, "course", "c2", "物理", "3"); code != 0 {
		t.Fatal("登记课程失败")
	}
	if out, _, code := runCLI(t, file, "req", "s1", "r1", "c1"); code != 0 {
		t.Fatalf("登记要求失败 out=%q", out)
	}
	if _, _, code := runCLI(t, file, "req", "s1", "r2", "c2"); code != 0 {
		t.Fatal("登记要求失败")
	}

	// 同一学生重复要求被拒绝。
	if _, errText, code := runCLI(t, file, "req", "s1", "r3", "c1"); code == 0 {
		t.Fatalf("重复课程要求应被拒绝，err=%q", errText)
	}

	// 修读：同学期两次，不同编号。
	if _, _, code := runCLI(t, file, "enroll", "s1", "r1", "2024春", "e1"); code != 0 {
		t.Fatal("选课 e1 失败")
	}
	if _, _, code := runCLI(t, file, "enroll", "s1", "r1", "2024春", "e2"); code != 0 {
		t.Fatal("选课 e2 失败")
	}
	// e1 未通过，e2 通过。
	if _, _, code := runCLI(t, file, "fail", "s1", "e1"); code != 0 {
		t.Fatal("提交未通过失败")
	}
	if _, _, code := runCLI(t, file, "pass", "s1", "e2"); code != 0 {
		t.Fatal("提交通过失败")
	}
	// 改结果应被拒绝。
	if _, errText, code := runCLI(t, file, "pass", "s1", "e1"); code == 0 {
		t.Fatalf("未通过改通过应被拒绝，err=%q", errText)
	}

	// r2 用免修满足。
	if out, _, code := runCLI(t, file, "waiver", "s1", "r2", "w1", "竞赛获奖"); code != 0 ||
		!strings.Contains(out, "有效") {
		t.Fatalf("免修应有效 code=%d out=%q", code, out)
	}

	// 重新打开进程核对：状态已持久化。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("核对失败 code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "总学分：7") {
		t.Fatalf("总学分应为 7（4+3），out=%q", out)
	}
	if !strings.Contains(out, "通过修读 e2") {
		t.Fatalf("应以通过修读 e2 说明 r1 来源，out=%q", out)
	}
	if !strings.Contains(out, "有效免修 w1") {
		t.Fatalf("应以免修 w1 说明 r2 来源，out=%q", out)
	}
	if strings.Contains(out, "未满足要求：[") {
		t.Fatalf("不应有未满足要求，out=%q", out)
	}

	// 停开课程后不能新增修读。
	if _, _, code := runCLI(t, file, "course-close", "c2"); code != 0 {
		t.Fatal("停开课程失败")
	}
	if _, errText, code := runCLI(t, file, "enroll", "s1", "r2", "2025春", "e3"); code == 0 {
		t.Fatalf("停开后新增修读应被拒绝，err=%q", errText)
	}
}

func TestCLIPersistenceAndRevoke(t *testing.T) {
	file := filepath.Join(t.TempDir(), "records.json")
	steps := [][]string{
		{"student", "s1"},
		{"course", "c1", "数学", "4"},
		{"req", "s1", "r1", "c1"},
		{"enroll", "s1", "r1", "2024春", "e1"},
		{"pass", "s1", "e1"},
		{"waiver", "s1", "r1", "w1", "依据"},
	}
	for _, st := range steps {
		if _, _, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 失败", st)
		}
	}

	// 通过与免修并存只计一次，核对以免修说明来源。
	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("核对失败：%s", out)
	}
	if !strings.Contains(out, "总学分：4") {
		t.Fatalf("只应计一份 4 学分，out=%q", out)
	}
	if !strings.Contains(out, "来源为有效免修 w1") {
		t.Fatalf("应以免修说明来源，out=%q", out)
	}
	if !strings.Contains(out, "通过修读历史") {
		t.Fatalf("应保留通过修读历史，out=%q", out)
	}

	// 撤销后有通过记录，继续满足。
	if _, _, code := runCLI(t, file, "revoke-waiver", "s1", "w1", "原因"); code != 0 {
		t.Fatal("撤销免修失败")
	}
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") {
		t.Fatalf("撤销后有通过记录应继续满足，code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "来源为通过修读 e1") {
		t.Fatalf("撤销后应改以通过修读说明来源，out=%q", out)
	}

	// show 能查到免修历史中的撤销状态。
	out, _, code = runCLI(t, file, "show", "s1")
	if code != 0 || !strings.Contains(out, "状态：已撤销") {
		t.Fatalf("show 应展示已撤销免修，code=%d out=%q", code, out)
	}
}

func TestCLIRejectedWaiverHistoryPersists(t *testing.T) {
	file := filepath.Join(t.TempDir(), "records.json")
	runCLI(t, file, "student", "s1")
	runCLI(t, file, "course", "c1", "数学", "4")

	// 目标要求不存在：拒绝（退出码 1）但内容与原因入历史并保存。
	outText, _, code := runCLI(t, file, "waiver", "s1", "rX", "wbad", "依据")
	if code != 1 || !strings.Contains(outText, "已拒绝") {
		t.Fatalf("不存在要求的免修应拒绝并退出码 1，code=%d out=%q", code, outText)
	}
	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("核对应成功：%s", out)
	}
	if !strings.Contains(out, "被拒绝的免修：") || !strings.Contains(out, "wbad") {
		t.Fatalf("被拒绝免修应出现在核对结果中，out=%q", out)
	}
}

// TestCLIRejectedWaiverResubmitAfterRequirementBuilt 命令行回归：免修申请先因
// 目标要求不存在被拒绝（退出码 1）；后来补建同编号要求，按原免修编号/原要求/
// 原依据再次提交时沿用重复提交的退出码 0，但显示状态仍为已拒绝——不是获批，
// 不新增申请，核对与历史继续展示同一条记录及具体原因。
func TestCLIRejectedWaiverResubmitAfterRequirementBuilt(t *testing.T) {
	file := filepath.Join(t.TempDir(), "records.json")
	runCLI(t, file, "student", "s1")
	runCLI(t, file, "student", "s2")
	runCLI(t, file, "course", "c1", "高等数学", "4")
	runCLI(t, file, "course", "c2", "物理", "3")
	// r1 先只在 s2 名下：对 s1 而言自己名下没有该要求。
	if _, _, code := runCLI(t, file, "req", "s2", "r1", "c1"); code != 0 {
		t.Fatal("为 s2 登记要求失败")
	}

	// 首次申请：s1 名下没有 r1，带非空依据，应拒绝（退出码 1）。
	out, _, code := runCLI(t, file, "waiver", "s1", "r1", "w1", "学科竞赛获奖")
	if code != 1 {
		t.Fatalf("目标要求不存在应退出码 1，得到 %d", code)
	}
	if !strings.Contains(out, "已拒绝") || !strings.Contains(out, "目标要求 r1 不存在") {
		t.Fatalf("首次申请应显示已拒绝及具体原因，out=%q", out)
	}

	// 后来补建 s1 自己的同编号要求。
	if _, _, code := runCLI(t, file, "req", "s1", "r1", "c1"); code != 0 {
		t.Fatal("为 s1 补建要求失败")
	}

	// 目标已存在后按原编号、原要求、原依据再次提交：退出码 0（重复提交成功），
	// 但状态仍为已拒绝，不能变成有效免修。
	out, _, code = runCLI(t, file, "waiver", "s1", "r1", "w1", "学科竞赛获奖")
	if code != 0 {
		t.Fatalf("相同内容重复提交应沿用退出码 0，得到 %d out=%q", code, out)
	}
	if !strings.Contains(out, "返回原申请") || !strings.Contains(out, "状态：已拒绝") {
		t.Fatalf("重复提交应返回原申请且状态仍为已拒绝，out=%q", out)
	}
	if strings.Contains(out, "有效") {
		t.Fatalf("重复提交不能被当成免修获批，out=%q", out)
	}

	// 纯幂等返回不应改动记录文件。
	snapshot, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, code := runCLI(t, file, "waiver", "s1", "r1", "w1", "学科竞赛获奖"); code != 0 {
		t.Fatal("再次重复提交应继续退出码 0")
	}
	if after, err := os.ReadFile(file); err != nil || string(after) != string(snapshot) {
		t.Fatal("幂等重复提交不应改动记录文件")
	}

	// 核对：要求未满足、总学分为零，旧申请不是学分来源；
	// 被拒绝记录继续展示同一免修编号、目标要求、依据和具体原因。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("核对应成功，code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "总学分：0") {
		t.Fatalf("旧拒绝申请不能成为学分来源，out=%q", out)
	}
	if !strings.Contains(out, "要求 r1") || !strings.Contains(out, "未满足") {
		t.Fatalf("r1 无通过无有效免修时应列为未满足，out=%q", out)
	}
	if strings.Contains(out, "来源为有效免修") || strings.Contains(out, "获得课程学分") {
		t.Fatalf("核对不应把旧申请列为有效免修来源，out=%q", out)
	}
	if !strings.Contains(out, "免修 w1（要求 r1，依据 \"学科竞赛获奖\"）") ||
		!strings.Contains(out, "目标要求 r1 不存在或不属于该学生") {
		t.Fatalf("核对中的拒绝记录应展示同一编号/要求/依据/原因，out=%q", out)
	}

	// 历史查询（show）同样保留。
	out, _, code = runCLI(t, file, "show", "s1")
	if code != 0 || !strings.Contains(out, "免修 w1：要求 r1，依据 \"学科竞赛获奖\"，状态：已拒绝") ||
		!strings.Contains(out, "目标要求 r1 不存在") {
		t.Fatalf("show 应继续展示同一条已拒绝记录及原因，code=%d out=%q", code, out)
	}

	// 同编号换另一项要求或另一份依据：内容冲突，明确拒绝，原结果保留。
	if _, _, code := runCLI(t, file, "req", "s1", "r2", "c2"); code != 0 {
		t.Fatal("登记 r2 失败")
	}
	if _, errText, code := runCLI(t, file, "waiver", "s1", "r2", "w1", "学科竞赛获奖"); code != 1 {
		t.Fatalf("同编号换要求应按冲突拒绝（退出码 1），code=%d err=%q", code, errText)
	}
	if _, errText, code := runCLI(t, file, "waiver", "s1", "r1", "w1", "另一份依据"); code != 1 {
		t.Fatalf("同编号换依据应按冲突拒绝（退出码 1），code=%d err=%q", code, errText)
	}
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "免修 w1（要求 r1，依据 \"学科竞赛获奖\"）：目标要求 r1 不存在") {
		t.Fatalf("冲突提交后原申请与原核对结果应保留，code=%d out=%q", code, out)
	}

	// 目标补建后用新编号正常申请新免修的既有行为不应受影响。
	if out, _, code := runCLI(t, file, "waiver", "s1", "r1", "w2", "新免修依据"); code != 0 ||
		!strings.Contains(out, "免修 w2 有效") {
		t.Fatalf("补建后新编号申请应正常有效，code=%d out=%q", code, out)
	}
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为有效免修 w2") ||
		!strings.Contains(out, "免修 w1（要求 r1，依据 \"学科竞赛获奖\"）") {
		t.Fatalf("新免修应带来学分，同时旧拒绝记录仍保留，code=%d out=%q", code, out)
	}
}

func TestCLIUnknownStudent(t *testing.T) {
	file := filepath.Join(t.TempDir(), "records.json")
	runCLI(t, file, "student", "s1")

	_, errText, code := runCLI(t, file, "check", "ghost")
	if code == 0 || !strings.Contains(errText, "不存在") {
		t.Fatalf("查询不存在学生应明确报错且非零退出，code=%d err=%q", code, errText)
	}
	_, errText, code = runCLI(t, file, "show", "ghost")
	if code == 0 || !strings.Contains(errText, "不存在") {
		t.Fatalf("show 不存在学生应明确报错，code=%d err=%q", code, errText)
	}
	// 引用不存在的学生登记要求/修读都应拒绝。
	runCLI(t, file, "course", "c1", "数学", "4")
	if _, errText, code := runCLI(t, file, "req", "ghost", "r1", "c1"); code == 0 {
		t.Fatalf("引用不存在学生应拒绝，err=%q", errText)
	}
	if _, errText, code := runCLI(t, file, "enroll", "s1", "r1", "2024春", "e1"); code == 0 {
		t.Fatalf("引用不存在要求应拒绝，err=%q", errText)
	}
}

func TestCLICorruptFileNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "records.json")
	if err := os.WriteFile(file, []byte("{ broken json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, errText, code := runCLI(t, file, "student", "s1")
	if code != exitFile || !strings.Contains(errText, "内容损坏") {
		t.Fatalf("损坏文件应报文件错误且退出码 %d，code=%d err=%q",
			exitFile, code, errText)
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "{ broken json" {
		t.Fatal("损坏文件不应被覆盖")
	}
}

func TestCLIReadOnlyDoesNotCreateFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "deep", "records.json")

	// 文件（及目录）不存在时，一次失败的查询不应凭空创建任何文件。
	if _, _, code := runCLI(t, file, "check", "ghost"); code == 0 {
		t.Fatal("不存在学生核对应失败")
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("只读失败命令不应创建记录文件，stat err=%v", err)
	}

	// 参数非法的登记命令同样不应创建文件。
	if _, _, code := runCLI(t, file, "student", " "); code == 0 {
		t.Fatal("空白编号应失败")
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("失败的登记命令不应创建记录文件")
	}
}

func TestCLIIdempotentCommandsDoNotGrow(t *testing.T) {
	file := filepath.Join(t.TempDir(), "records.json")
	runCLI(t, file, "student", "s1")
	first, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	// 纯幂等重复操作后文件内容不变。
	for _, args := range [][]string{
		{"student", "s1"},
		{"check", "s1"},
		{"show", "s1"},
	} {
		if _, _, code := runCLI(t, file, args...); code != 0 {
			t.Fatalf("命令 %v 失败", args)
		}
	}
	// check/show 不产生变更；student 重复也不产生变更。
	second, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("幂等命令不应改变记录文件\nfirst=%s\nsecond=%s", first, second)
	}
}
