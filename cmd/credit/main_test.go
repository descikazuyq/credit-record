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

// setupCLISharedEnrollments 建立两名学生就同一门 4 学分课程的同号要求 r1 与
// 同一学期的同号修读 e1，并断言两份修读都成功登记、初始为选课、各自核对均为
// 0 学分且要求未满足。
func setupCLISharedEnrollments(t *testing.T, file string) {
	t.Helper()
	for _, st := range [][]string{
		{"student", "s1"},
		{"student", "s2"},
		{"course", "c1", "高等数学", "4"},
		{"req", "s1", "r1", "c1"},
		{"req", "s2", "r1", "c1"},
		{"enroll", "s1", "r1", "2024春", "e1"},
		{"enroll", "s2", "r1", "2024春", "e1"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}
	for _, student := range []string{"s1", "s2"} {
		out, _, code := runCLI(t, file, "show", student)
		if code != 0 || !strings.Contains(out, "修读 e1：要求 r1，学期 2024春，结果：选课") {
			t.Fatalf("学生 %s 的同号修读应已登记且初始为选课，code=%d out=%q",
				student, code, out)
		}
		out, _, code = runCLI(t, file, "check", student)
		if code != 0 || !strings.Contains(out, "总学分：0") ||
			!strings.Contains(out, "未满足要求：[r1]") {
			t.Fatalf("学生 %s 初始核对应为 0 学分且要求未满足，code=%d out=%q",
				student, code, out)
		}
	}
}

// TestCLISharedEnrollmentResultOwnership 两名学生共享要求编号与修读编号时，
// 命令行提交结果必须各归各：s1 通过只影响 s1，s2 随后未通过只影响 s2。
func TestCLISharedEnrollmentResultOwnership(t *testing.T) {
	file := filepath.Join(t.TempDir(), "records.json")
	setupCLISharedEnrollments(t, file)

	// s1 提交通过：退出码 0，只有 s1 获得 4 学分、要求满足、来源是本人的 e1。
	if _, errText, code := runCLI(t, file, "pass", "s1", "e1"); code != 0 {
		t.Fatalf("s1 首次提交通过应成功且退出码 0，code=%d err=%q", code, errText)
	}
	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "要求 r1") || !strings.Contains(out, "已满足") ||
		!strings.Contains(out, "来源为通过修读 e1") {
		t.Fatalf("s1 应得 4 学分且要求由本人 e1 满足，code=%d out=%q", code, out)
	}
	if strings.Contains(out, "未满足要求：[") {
		t.Fatalf("s1 不应再有未满足要求，out=%q", out)
	}
	// s2 仍是选课：不能因为编号相同拿到学分或被改成通过。
	out, _, code = runCLI(t, file, "check", "s2")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "未满足要求：[r1]") || strings.Contains(out, "通过修读") {
		t.Fatalf("s2 不应因 s1 的同号通过拿到学分，code=%d out=%q", code, out)
	}
	if out, _, _ := runCLI(t, file, "show", "s2"); !strings.Contains(out, "结果：选课") {
		t.Fatalf("s2 的同号修读应保持选课，out=%q", out)
	}

	// s2 随后提交未通过：退出码 0，s2 仍为 0 学分、要求未满足。
	if _, errText, code := runCLI(t, file, "fail", "s2", "e1"); code != 0 {
		t.Fatalf("s2 首次提交未通过应成功且退出码 0，code=%d err=%q", code, errText)
	}
	out, _, code = runCLI(t, file, "check", "s2")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "未满足要求：[r1]") {
		t.Fatalf("s2 未通过后应仍为 0 学分且要求未满足，code=%d out=%q", code, out)
	}
	if out, _, _ := runCLI(t, file, "show", "s2"); !strings.Contains(out, "结果：未通过") {
		t.Fatalf("s2 的修读应记录为未通过，out=%q", out)
	}
	// s1 原来的通过结果、学分与来源均保持不变。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为通过修读 e1") {
		t.Fatalf("s2 提交未通过不应影响 s1 原有的通过结果，code=%d out=%q", code, out)
	}
	if out, _, _ := runCLI(t, file, "show", "s1"); !strings.Contains(out, "结果：通过") {
		t.Fatalf("s1 的修读应保持通过，out=%q", out)
	}

	// 两人结果不同应分别被接受：各自重复提交相同结果幂等成功（退出码 0），
	// 不能被误判为同一次修读的结果冲突；状态不变。
	if _, errText, code := runCLI(t, file, "pass", "s1", "e1"); code != 0 {
		t.Fatalf("s1 重复提交通过应幂等成功，code=%d err=%q", code, errText)
	}
	if _, errText, code := runCLI(t, file, "fail", "s2", "e1"); code != 0 {
		t.Fatalf("s2 重复提交未通过应幂等成功，code=%d err=%q", code, errText)
	}
	out, _, _ = runCLI(t, file, "check", "s1")
	if !strings.Contains(out, "总学分：4") || !strings.Contains(out, "来源为通过修读 e1") {
		t.Fatalf("幂等重复后 s1 结果应不变，out=%q", out)
	}
	out, _, _ = runCLI(t, file, "check", "s2")
	if !strings.Contains(out, "总学分：0") || !strings.Contains(out, "未满足要求：[r1]") {
		t.Fatalf("幂等重复后 s2 结果应不变，out=%q", out)
	}

	// 各自把本人这份修读改成另一结果：仍按现有规则拒绝（退出码 1）、保留原结果，
	// 另一名学生的同号修读保持原样。
	if _, errText, code := runCLI(t, file, "fail", "s1", "e1"); code != 1 {
		t.Fatalf("s1 通过后改提未通过应拒绝且退出码 1，code=%d err=%q", code, errText)
	}
	if _, errText, code := runCLI(t, file, "pass", "s2", "e1"); code != 1 {
		t.Fatalf("s2 未通过后改提通过应拒绝且退出码 1，code=%d err=%q", code, errText)
	}
	if out, _, _ := runCLI(t, file, "show", "s1"); !strings.Contains(out, "结果：通过") {
		t.Fatalf("被拒绝后 s1 应保留原通过结果，out=%q", out)
	}
	if out, _, _ := runCLI(t, file, "show", "s2"); !strings.Contains(out, "结果：未通过") {
		t.Fatalf("被拒绝后 s2 应保留原未通过结果，out=%q", out)
	}
	out, _, _ = runCLI(t, file, "check", "s1")
	if !strings.Contains(out, "总学分：4") || !strings.Contains(out, "来源为通过修读 e1") {
		t.Fatalf("改结果被拒后 s1 学分与来源应不变，out=%q", out)
	}
	out, _, _ = runCLI(t, file, "check", "s2")
	if !strings.Contains(out, "总学分：0") || !strings.Contains(out, "未满足要求：[r1]") {
		t.Fatalf("改结果被拒后 s2 应仍为 0 学分未满足，out=%q", out)
	}
}

// TestCLISharedEnrollmentResultOwnershipReversed 交换两名学生的登记与处理顺序：
// 先 s2 未通过、再 s1 通过，归属结论必须一致，不依赖先登记或先处理谁。
func TestCLISharedEnrollmentResultOwnershipReversed(t *testing.T) {
	file := filepath.Join(t.TempDir(), "records.json")
	// 先登记 s2，再登记 s1。
	for _, st := range [][]string{
		{"student", "s2"},
		{"student", "s1"},
		{"course", "c1", "高等数学", "4"},
		{"req", "s2", "r1", "c1"},
		{"req", "s1", "r1", "c1"},
		{"enroll", "s2", "r1", "2024春", "e1"},
		{"enroll", "s1", "r1", "2024春", "e1"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}

	if _, _, code := runCLI(t, file, "fail", "s2", "e1"); code != 0 {
		t.Fatal("先处理的 s2 提交未通过应成功")
	}
	if _, _, code := runCLI(t, file, "pass", "s1", "e1"); code != 0 {
		t.Fatal("后处理的 s1 提交通过应成功")
	}

	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为通过修读 e1") {
		t.Fatalf("s1 应由本人 e1 通过获得 4 学分，code=%d out=%q", code, out)
	}
	if out, _, _ := runCLI(t, file, "show", "s1"); !strings.Contains(out, "结果：通过") {
		t.Fatalf("s1 的修读应为通过，out=%q", out)
	}
	out, _, code = runCLI(t, file, "check", "s2")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "未满足要求：[r1]") {
		t.Fatalf("s2 应仍为 0 学分且要求未满足，code=%d out=%q", code, out)
	}
	if out, _, _ := runCLI(t, file, "show", "s2"); !strings.Contains(out, "结果：未通过") {
		t.Fatalf("s2 的修读应为未通过，out=%q", out)
	}
}

// setupCLISharedWaiverReqs 在记录文件中建立两名学生各一项同编号要求 r1：
// s1 的 r1 指向 4 学分的 c1，s2 的 r1 指向 3 学分的 c2；两人均无修读。
func setupCLISharedWaiverReqs(t *testing.T, file string) {
	t.Helper()
	for _, st := range [][]string{
		{"student", "s1"},
		{"student", "s2"},
		{"course", "c1", "高等数学", "4"},
		{"course", "c2", "大学物理", "3"},
		{"req", "s1", "r1", "c1"},
		{"req", "s2", "r1", "c2"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}
}

// assertCLIWaiverOwner 跨进程核对该学生：要求 r1 已满足、学分为本人课程学分、
// 来源是本人名下的有效免修 w1，且 show 中只呈现本人的依据。
func assertCLIWaiverOwner(t *testing.T, file, student, course, courseName, credit, basis string) {
	t.Helper()
	out, _, code := runCLI(t, file, "check", student)
	if code != 0 {
		t.Fatalf("学生 %s 核对失败 code=%d out=%q", student, code, out)
	}
	want := []string{
		"总学分：" + credit,
		"要求 r1（课程 " + course + "《" + courseName + "》，" + credit + " 学分）：已满足，来源为有效免修 w1",
	}
	for _, frag := range want {
		if !strings.Contains(out, frag) {
			t.Fatalf("学生 %s 的核对结果应包含 %q，out=%q", student, frag, out)
		}
	}
	if strings.Contains(out, "未满足要求：[") {
		t.Fatalf("学生 %s 不应有未满足要求，out=%q", student, out)
	}
	show, _, code := runCLI(t, file, "show", student)
	if code != 0 {
		t.Fatalf("学生 %s show 失败 code=%d out=%q", student, code, show)
	}
	if !strings.Contains(show, "要求 r1 -> 课程 "+course) ||
		!strings.Contains(show, `免修 w1：要求 r1，依据 "`+basis+`"，状态：有效`) {
		t.Fatalf("学生 %s 的记录应分清本人的课程与依据，show=%q", student, show)
	}
}

// TestCLISharedWaiverIDsOwnership 两名学生的要求与免修编号都相同（r1/w1），
// 分别指向 4 学分与 3 学分课程。每次命令都是独立进程（重新加载记录文件），
// 验证不论谁先提交，同号申请各归各、先后正常生效、学分来源分别为本人的 w1。
func TestCLISharedWaiverIDsOwnership(t *testing.T) {
	const basisS1 = "学科竞赛获奖"
	const basisS2 = "外校同层次课程"
	for _, first := range []string{"s1", "s2"} {
		second := "s2"
		if first == "s2" {
			second = "s1"
		}
		t.Run(first+"先提交", func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "records.json")
			setupCLISharedWaiverReqs(t, file)
			basis := map[string]string{"s1": basisS1, "s2": basisS2}
			course := map[string]string{"s1": "c1", "s2": "c2"}
			courseName := map[string]string{"s1": "高等数学", "s2": "大学物理"}
			credit := map[string]string{"s1": "4", "s2": "3"}

			// 先提交者：申请有效，只能使本人的 r1 满足。
			out, _, code := runCLI(t, file, "waiver", first, "r1", "w1", basis[first])
			if code != 0 || !strings.Contains(out, "免修 w1 有效") {
				t.Fatalf("%s 首次申请应有效且退出码 0，code=%d out=%q", first, code, out)
			}
			assertCLIWaiverOwner(t, file, first, course[first], courseName[first],
				credit[first], basis[first])

			// 另一人：要求仍未满足、0 学分，记录中看不到前一人的申请。
			out, _, code = runCLI(t, file, "check", second)
			if code != 0 || !strings.Contains(out, "总学分：0") ||
				!strings.Contains(out, "未满足要求：[r1]") ||
				strings.Contains(out, "有效免修") {
				t.Fatalf("%s 不应因 %s 的同号免修获得学分，code=%d out=%q",
					second, first, code, out)
			}
			if show, _, _ := runCLI(t, file, "show", second); strings.Contains(show, "免修 w1") {
				t.Fatalf("%s 的免修历史中不应出现 %s 的申请，show=%q", second, first, show)
			}

			// 另一人随后用同编号 w1 提交本人的非空依据：正常生效，
			// 不能被判为重复提交、编号冲突或要求已免修。
			out, _, code = runCLI(t, file, "waiver", second, "r1", "w1", basis[second])
			if code != 0 || !strings.Contains(out, "免修 w1 有效") {
				t.Fatalf("%s 的同号申请应独立生效，code=%d out=%q", second, code, out)
			}

			// 两人核对结果分别为 4 学分和 3 学分，来源均为本人名下的 w1，
			// show 中各自只保留本人依据；编号相同仍能分清课程与依据。
			assertCLIWaiverOwner(t, file, "s1", "c1", "高等数学", "4", basisS1)
			assertCLIWaiverOwner(t, file, "s2", "c2", "大学物理", "3", basisS2)
			showS1, _, _ := runCLI(t, file, "show", "s1")
			showS2, _, _ := runCLI(t, file, "show", "s2")
			if strings.Contains(showS1, basisS2) || strings.Contains(showS2, basisS1) {
				t.Fatalf("两人的依据应各留本人名下：\nshow s1=%q\nshow s2=%q", showS1, showS2)
			}
		})
	}
}

// TestCLISameStudentWaiverRulesKeptAcrossSharedIDs 两名学生各持同号有效免修后，
// 同一学生名下的既有限制通过独立进程逐一步骤仍成立：原样重复幂等；同编号
// 换依据按内容冲突拒绝；新编号 w2 取代已免修的 r1 被拒绝并留下独立拒绝记录，
// 核对文本必须对应到 w2 这次申请并说明已有有效免修 w1；另一人全程不变。
func TestCLISameStudentWaiverRulesKeptAcrossSharedIDs(t *testing.T) {
	file := filepath.Join(t.TempDir(), "records.json")
	setupCLISharedWaiverReqs(t, file)
	const basisS1 = "学科竞赛获奖"
	const basisS2 = "外校同层次课程"

	if _, _, code := runCLI(t, file, "waiver", "s1", "r1", "w1", basisS1); code != 0 {
		t.Fatal("s1 首次免修应成功")
	}
	if _, _, code := runCLI(t, file, "waiver", "s2", "r1", "w1", basisS2); code != 0 {
		t.Fatal("s2 的同号免修应独立成功")
	}

	// 原编号、原要求、原依据再次提交：返回本人原申请（退出码 0），
	// 历史条数与学分不增加。
	out, _, code := runCLI(t, file, "waiver", "s1", "r1", "w1", basisS1)
	if code != 0 || !strings.Contains(out, "已提交过且内容一致，返回原申请，状态：有效") {
		t.Fatalf("原样重复提交应幂等返回原有效申请，code=%d out=%q", code, out)
	}
	assertCLIWaiverOwner(t, file, "s1", "c1", "高等数学", "4", basisS1)
	assertCLIWaiverOwner(t, file, "s2", "c2", "大学物理", "3", basisS2)

	// 在原 w1 下改换依据：按内容冲突拒绝（退出码 1），不新增申请。
	if _, errText, code := runCLI(t, file, "waiver", "s1", "r1", "w1", "改换后的依据"); code != 1 {
		t.Fatalf("同编号换依据应拒绝且退出码 1，code=%d err=%q", code, errText)
	}
	// 原依据与有效状态保留，学分仍是 4。
	assertCLIWaiverOwner(t, file, "s1", "c1", "高等数学", "4", basisS1)
	// 另一人的同号记录不能被改动。
	assertCLIWaiverOwner(t, file, "s2", "c2", "大学物理", "3", basisS2)

	// 用新免修编号 w2 再次取代已免修的 r1：拒绝（退出码 1），但在提交者
	// 历史中留下独立的拒绝记录。
	out, _, code = runCLI(t, file, "waiver", "s1", "r1", "w2", "再次申请的新依据")
	if code != 1 || !strings.Contains(out, "免修申请 w2 已拒绝") {
		t.Fatalf("新编号取代已免修要求应拒绝并记入历史，code=%d out=%q", code, out)
	}

	// 核对结果：r1 仍由原 w1 满足、4 学分只计一次；拒绝清单对应到 w2 这次
	// 申请（编号、目标要求、依据），并说明该要求已有有效免修 w1 的具体原因。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为有效免修 w1") {
		t.Fatalf("原有效免修应继续作为唯一学分来源，code=%d out=%q", code, out)
	}
	for _, frag := range []string{
		"被拒绝的免修：",
		`免修 w2（要求 r1，依据 "再次申请的新依据"）`,
		"该要求已有有效免修 w1",
	} {
		if !strings.Contains(out, frag) {
			t.Fatalf("核对结果应包含 %q，out=%q", frag, out)
		}
	}
	// show 同样保留两条历史：w1 有效、w2 已拒绝且带具体原因。
	show, _, code := runCLI(t, file, "show", "s1")
	if code != 0 ||
		!strings.Contains(show, `免修 w1：要求 r1，依据 "`+basisS1+`"，状态：有效`) ||
		!strings.Contains(show, `免修 w2：要求 r1，依据 "再次申请的新依据"，状态：已拒绝`) ||
		!strings.Contains(show, "该要求已有有效免修 w1") {
		t.Fatalf("s1 历史应保留有效的 w1 与独立拒绝的 w2，code=%d show=%q", code, show)
	}

	// 被拒绝的 w2 原样重复提交：返回原拒绝记录（退出码 0），不新增不生效。
	out, _, code = runCLI(t, file, "waiver", "s1", "r1", "w2", "再次申请的新依据")
	if code != 0 || !strings.Contains(out, "已提交过且内容一致，返回原申请，状态：已拒绝") {
		t.Fatalf("重复提交被拒绝的 w2 应幂等返回原记录，code=%d out=%q", code, out)
	}

	// 另一人的免修历史、有效申请与核对结果保持原样：只有本人的 w1，
	// 看不到 s1 的 w2，仍是 3 学分。
	out, _, code = runCLI(t, file, "check", "s2")
	if code != 0 || !strings.Contains(out, "总学分：3") ||
		!strings.Contains(out, "来源为有效免修 w1") ||
		strings.Contains(out, "被拒绝的免修") || strings.Contains(out, "w2") {
		t.Fatalf("s2 应保持 3 学分且无 s1 的拒绝记录，code=%d out=%q", code, out)
	}
	show, _, _ = runCLI(t, file, "show", "s2")
	if !strings.Contains(show, `免修 w1：要求 r1，依据 "`+basisS2+`"，状态：有效`) ||
		strings.Contains(show, "w2") || strings.Contains(show, basisS1) {
		t.Fatalf("s2 的历史应只有本人的 w1，show=%q", show)
	}
}

// TestCLISubmitResultUnknownEnrollmentUnderOtherStudent 修读编号只存在于 s1 名下时，
// 用 s2 的名义提交结果必须明确报告 s2 名下没有该修读、退出码 1，不能借用 s1 的
// 修读，也不能为 s2 补出新记录；拒绝后两人的状态、要求与总学分与提交前一致，
// 记录文件也不应被这次拒绝改写。
func TestCLISubmitResultUnknownEnrollmentUnderOtherStudent(t *testing.T) {
	file := filepath.Join(t.TempDir(), "records.json")
	for _, st := range [][]string{
		{"student", "s1"},
		{"student", "s2"},
		{"course", "c1", "高等数学", "4"},
		{"req", "s1", "r1", "c1"},
		{"enroll", "s1", "r1", "2024春", "e1"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}

	// 用 s2 的编号提交只属于 s1 的修读结果：退出码 1，信息点名 s2 与 e1。
	out, errText, code := runCLI(t, file, "pass", "s2", "e1")
	if code != 1 {
		t.Fatalf("借名提交应拒绝且退出码 1，code=%d out=%q err=%q", code, out, errText)
	}
	if !strings.Contains(errText, "s2") || !strings.Contains(errText, "e1") ||
		!strings.Contains(errText, "不存在") {
		t.Fatalf("应明确报告学生 s2 名下不存在修读 e1，out=%q err=%q", out, errText)
	}

	// 被拒绝的请求不应改写记录文件。
	after, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("拒绝后记录文件不应变化\nbefore=%s\nafter=%s", before, after)
	}

	// 不能借用 s1 的修读：s1 的 e1 仍是选课、0 学分、要求未满足。
	if out, _, _ := runCLI(t, file, "show", "s1"); !strings.Contains(out, "结果：选课") {
		t.Fatalf("s1 的修读不应被借名提交改写，out=%q", out)
	}
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "未满足要求：[r1]") {
		t.Fatalf("s1 拒绝后应与提交前一致（0 学分未满足），code=%d out=%q", code, out)
	}
	// 不能为 s2 补出一条新记录：show 中没有任何修读，核对总学分 0。
	if out, _, _ := runCLI(t, file, "show", "s2"); strings.Contains(out, "修读 e1") {
		t.Fatalf("不应在 s2 名下补出修读记录，out=%q", out)
	}
	out, _, code = runCLI(t, file, "check", "s2")
	if code != 0 || !strings.Contains(out, "总学分：0") {
		t.Fatalf("s2 不应因拒绝产生任何变化，code=%d out=%q", code, out)
	}
}
