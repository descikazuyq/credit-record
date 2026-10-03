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

func TestCLIRejectedWaiverResubmitAfterReqCreated(t *testing.T) {
	file := filepath.Join(t.TempDir(), "records.json")
	runCLI(t, file, "student", "s1")
	runCLI(t, file, "course", "c1", "数学", "4")

	// 首次申请：目标要求不存在，退出码 1，内容与原因记入免修历史。
	out, _, code := runCLI(t, file, "waiver", "s1", "r1", "w1", "学科竞赛获奖")
	if code != 1 || !strings.Contains(out, "已拒绝") {
		t.Fatalf("目标要求不存在的首次申请应拒绝且退出码 1，code=%d out=%q", code, out)
	}

	// 后来补建同编号要求。
	if _, _, code := runCLI(t, file, "req", "s1", "r1", "c1"); code != 0 {
		t.Fatal("补建要求失败")
	}

	// 按相同内容再次提交：沿用重复提交成功的退出码 0，但状态仍为已拒绝，
	// 不能被当成免修获批。重复执行结果不变。
	for i := 0; i < 2; i++ {
		out, _, code := runCLI(t, file, "waiver", "s1", "r1", "w1", "学科竞赛获奖")
		if code != 0 {
			t.Fatalf("第 %d 次重复提交应幂等成功，code=%d out=%q", i+1, code, out)
		}
		if !strings.Contains(out, "已拒绝") || strings.Contains(out, "有效") {
			t.Fatalf("重复提交应显示原申请仍为已拒绝，out=%q", out)
		}
	}

	// 核对：要求仍未满足，总学分为零，被拒绝记录展示原编号、要求、依据与原因。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("核对失败 code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "总学分：0") {
		t.Fatalf("被拒绝的旧申请不能成为学分来源，out=%q", out)
	}
	if !strings.Contains(out, "未满足要求：[r1]") {
		t.Fatalf("要求 r1 应仍列为未满足，out=%q", out)
	}
	if !strings.Contains(out, "被拒绝的免修：") ||
		!strings.Contains(out, `免修 w1（要求 r1，依据 "学科竞赛获奖"）`) ||
		!strings.Contains(out, "目标要求 r1 不存在") {
		t.Fatalf("核对中的被拒绝记录应保留原编号、要求、依据与原因，out=%q", out)
	}

	// 历史查询同样展示原记录与原因。
	out, _, code = runCLI(t, file, "show", "s1")
	if code != 0 {
		t.Fatalf("show 失败 code=%d out=%q", code, out)
	}
	if !strings.Contains(out, `免修 w1：要求 r1，依据 "学科竞赛获奖"，状态：已拒绝`) ||
		!strings.Contains(out, "目标要求 r1 不存在") {
		t.Fatalf("show 应展示原拒绝记录与原因，out=%q", out)
	}

	// 同编号换要求或换依据：按内容冲突明确拒绝，原申请及原核对结果保留。
	runCLI(t, file, "course", "c2", "物理", "3")
	runCLI(t, file, "req", "s1", "r2", "c2")
	if _, errText, code := runCLI(t, file, "waiver", "s1", "r2", "w1", "学科竞赛获奖"); code != 1 {
		t.Fatalf("同编号换要求应按冲突拒绝，code=%d err=%q", code, errText)
	}
	if _, errText, code := runCLI(t, file, "waiver", "s1", "r1", "w1", "另一份依据"); code != 1 {
		t.Fatalf("同编号换依据应按冲突拒绝，code=%d err=%q", code, errText)
	}
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "目标要求 r1 不存在") {
		t.Fatalf("冲突拒绝不应改变原核对结果，code=%d out=%q", code, out)
	}

	// 目标补建后，用新编号正常申请免修的既有行为不变。
	out, _, code = runCLI(t, file, "waiver", "s1", "r1", "w2", "外校同层次课程")
	if code != 0 || !strings.Contains(out, "有效") {
		t.Fatalf("补建要求后新编号申请应正常生效，code=%d out=%q", code, out)
	}
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为有效免修 w2") {
		t.Fatalf("新免修应正常计学分，code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "免修 w1") || !strings.Contains(out, "目标要求 r1 不存在") {
		t.Fatalf("旧拒绝记录应继续保留在核对结果中，out=%q", out)
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

// 两名学生共享要求编号与修读编号时，提交结果必须严格按学生归属：
// 一人通过不影响另一人，一人未通过也不与另一人的通过冲突。
func TestCLICrossStudentSharedEnrollmentIDs(t *testing.T) {
	file := filepath.Join(t.TempDir(), "records.json")

	// 两名学生为同一门 4 学分课程建立同号要求，同学期使用相同修读编号。
	steps := [][]string{
		{"student", "s1"},
		{"student", "s2"},
		{"course", "c1", "数学", "4"},
		{"req", "s1", "r1", "c1"},
		{"req", "s2", "r1", "c1"},
		{"enroll", "s1", "r1", "2024春", "e1"},
		{"enroll", "s2", "r1", "2024春", "e1"},
	}
	for _, st := range steps {
		if _, _, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功（退出码 0）", st)
		}
	}

	// 初始：两人各自核对均为 0 学分、要求未满足，修读状态都是选课。
	for _, st := range []string{"s1", "s2"} {
		out, _, code := runCLI(t, file, "check", st)
		if code != 0 || !strings.Contains(out, "总学分：0") ||
			!strings.Contains(out, "未满足要求：[r1]") {
			t.Fatalf("%s 初始核对应为 0 学分且 r1 未满足，code=%d out=%q", st, code, out)
		}
		out, _, code = runCLI(t, file, "show", st)
		if code != 0 || !strings.Contains(out, "修读 e1：要求 r1，学期 2024春，结果：选课") {
			t.Fatalf("%s 的修读 e1 初始应为选课，code=%d out=%q", st, code, out)
		}
	}

	// s1 的 e1 提交通过，退出码 0。
	if _, _, code := runCLI(t, file, "pass", "s1", "e1"); code != 0 {
		t.Fatal("首次提交通过应返回退出码 0")
	}
	// 只有 s1 获得 4 学分，要求满足且来源指向本人这份修读。
	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为通过修读 e1") ||
		!strings.Contains(out, "未满足要求：（无）") {
		t.Fatalf("s1 应获得 4 学分且来源为本人修读 e1，code=%d out=%q", code, out)
	}
	// s2 不能因编号相同拿到学分或被改成通过：仍是选课、0 学分、未满足。
	out, _, code = runCLI(t, file, "check", "s2")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "未满足要求：[r1]") {
		t.Fatalf("s2 应仍为 0 学分且未满足，code=%d out=%q", code, out)
	}
	out, _, code = runCLI(t, file, "show", "s2")
	if code != 0 || !strings.Contains(out, "结果：选课") {
		t.Fatalf("s2 的修读不应被 s1 的提交改动，code=%d out=%q", code, out)
	}

	// s2 的同号修读提交未通过：与 s1 的通过是两次不同修读的结果，
	// 应分别被接受（退出码 0），不能误判为同一次修读的结果冲突。
	if _, _, code := runCLI(t, file, "fail", "s2", "e1"); code != 0 {
		t.Fatal("s2 的同号修读提交未通过应被接受（退出码 0）")
	}
	// s2 仍为 0 学分、要求未满足。
	out, _, code = runCLI(t, file, "check", "s2")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "未满足要求：[r1]") {
		t.Fatalf("s2 未通过后应仍为 0 学分且未满足，code=%d out=%q", code, out)
	}
	// s1 原来的通过结果、学分与来源均保持不变。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为通过修读 e1") {
		t.Fatalf("s1 的通过结果与学分应保持不变，code=%d out=%q", code, out)
	}
	// 两人的原始修读状态可明确区分：一个通过、一个未通过。
	out, _, _ = runCLI(t, file, "show", "s1")
	if !strings.Contains(out, "修读 e1：要求 r1，学期 2024春，结果：通过") {
		t.Fatalf("s1 的修读应保持通过，out=%q", out)
	}
	out, _, _ = runCLI(t, file, "show", "s2")
	if !strings.Contains(out, "修读 e1：要求 r1，学期 2024春，结果：未通过") {
		t.Fatalf("s2 的修读应为未通过，out=%q", out)
	}

	// 各自已提交结果后改提另一结果：拒绝（退出码 1），双方原记录保留。
	if _, _, code := runCLI(t, file, "fail", "s1", "e1"); code != 1 {
		t.Fatal("s1 已通过后改提未通过应被拒绝（退出码 1）")
	}
	if _, _, code := runCLI(t, file, "pass", "s2", "e1"); code != 1 {
		t.Fatal("s2 已未通过后改提通过应被拒绝（退出码 1）")
	}
	out, _, _ = runCLI(t, file, "show", "s1")
	if !strings.Contains(out, "结果：通过") {
		t.Fatalf("拒绝后 s1 的原结果应保留为通过，out=%q", out)
	}
	out, _, _ = runCLI(t, file, "show", "s2")
	if !strings.Contains(out, "结果：未通过") {
		t.Fatalf("拒绝后 s2 的原结果应保留为未通过，out=%q", out)
	}
	out, _, _ = runCLI(t, file, "check", "s1")
	if !strings.Contains(out, "总学分：4") {
		t.Fatalf("拒绝后 s1 的学分应保持 4，out=%q", out)
	}
	out, _, _ = runCLI(t, file, "check", "s2")
	if !strings.Contains(out, "总学分：0") {
		t.Fatalf("拒绝后 s2 的学分应保持 0，out=%q", out)
	}
}

// 修读编号只存在于第一名学生名下时，用第二名学生的编号提交结果必须
// 明确拒绝：不借用他人修读，也不为第二名学生补出新记录。
func TestCLISubmitResultNotBorrowedAcrossStudents(t *testing.T) {
	file := filepath.Join(t.TempDir(), "records.json")
	steps := [][]string{
		{"student", "s1"},
		{"student", "s2"},
		{"course", "c1", "数学", "4"},
		{"req", "s1", "r1", "c1"},
		{"enroll", "s1", "r1", "2024春", "e1"},
	}
	for _, st := range steps {
		if _, _, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 失败", st)
		}
	}

	// e1 只在 s1 名下：用 s2 提交结果应明确报告 s2 名下没有该修读，退出码 1。
	_, errText, code := runCLI(t, file, "pass", "s2", "e1")
	if code != 1 || !strings.Contains(errText, "学生 s2 名下不存在修读 e1") {
		t.Fatalf("应明确报告 s2 名下没有该修读且退出码 1，code=%d err=%q", code, errText)
	}

	// 不为 s2 补出一条新记录。
	out, _, code := runCLI(t, file, "show", "s2")
	if code != 0 || !strings.Contains(out, "修读：（无）") {
		t.Fatalf("拒绝后 s2 名下不应出现修读记录，code=%d out=%q", code, out)
	}
	// 也不借用 s1 的修读：s1 的 e1 仍是选课。
	out, _, code = runCLI(t, file, "show", "s1")
	if code != 0 || !strings.Contains(out, "修读 e1：要求 r1，学期 2024春，结果：选课") {
		t.Fatalf("s1 的修读不应被改动，code=%d out=%q", code, out)
	}
	// 两人的要求满足情况和总学分都与提交前一致。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "未满足要求：[r1]") {
		t.Fatalf("s1 的核对应与提交前一致，code=%d out=%q", code, out)
	}
	out, _, code = runCLI(t, file, "check", "s2")
	if code != 0 || !strings.Contains(out, "总学分：0") {
		t.Fatalf("s2 的核对应与提交前一致，code=%d out=%q", code, out)
	}
}
