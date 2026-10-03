package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tempRecordFile(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "records.json")
}

// 免修编号与要求编号都只在所属学生名下唯一：以下命令行端到端用例重复检查
// 两名学生提交同号免修申请时的归属、有效状态、学分来源与免修历史隔离，
// 以及同一学生名下既有申请规则保持现有行为。每条命令都重新打开记录文件，
// 因此这些检查同时覆盖了落盘与重新加载后的归属。

// setupCLISharedWaivers 登记两名学生：各自有一项编号 r1 的要求，分别指向
// 4 学分和 3 学分的课程，两人都没有修读；各自初始核对均为 0 学分、未满足。
func setupCLISharedWaivers(t *testing.T, file string, s2First bool) {
	t.Helper()
	steps := [][]string{
		{"student", "s1"},
		{"student", "s2"},
		{"course", "c1", "高等数学", "4"},
		{"course", "c2", "线性代数", "3"},
		{"req", "s1", "r1", "c1"},
		{"req", "s2", "r1", "c2"},
	}
	if s2First {
		steps = [][]string{
			{"student", "s2"},
			{"student", "s1"},
			{"course", "c2", "线性代数", "3"},
			{"course", "c1", "高等数学", "4"},
			{"req", "s2", "r1", "c2"},
			{"req", "s1", "r1", "c1"},
		}
	}
	for _, st := range steps {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}
	for _, student := range []string{"s1", "s2"} {
		out, _, code := runCLI(t, file, "check", student)
		if code != 0 || !strings.Contains(out, "总学分：0") ||
			!strings.Contains(out, "未满足要求：[r1]") {
			t.Fatalf("学生 %s 初始核对应为 0 学分且 r1 未满足，code=%d out=%q",
				student, code, out)
		}
		out, _, _ = runCLI(t, file, "show", student)
		if !strings.Contains(out, "免修历史：（无）") {
			t.Fatalf("学生 %s 初始不应有免修历史，out=%q", student, out)
		}
	}
}

// assertCLIOwnerWaiver 核对某名学生的 r1 由本人名下的 w1 满足：
// 学分为 wantCredit、来源是本人的 w1、要求指向本人课程、历史只含本人依据。
func assertCLIOwnerWaiver(t *testing.T, file, student, wantCredit, course, basis string) {
	t.Helper()
	out, _, code := runCLI(t, file, "check", student)
	if code != 0 || !strings.Contains(out, "总学分："+wantCredit) {
		t.Fatalf("学生 %s 应有 %s 学分，code=%d out=%q", student, wantCredit, code, out)
	}
	if strings.Contains(out, "未满足要求：[") {
		t.Fatalf("学生 %s 的 r1 应已满足，out=%q", student, out)
	}
	if !strings.Contains(out, "要求 r1（课程 "+course) ||
		!strings.Contains(out, "来源为有效免修 w1") {
		t.Fatalf("学生 %s 的 r1 应指向课程 %s 并由本人 w1 满足，out=%q",
			student, course, out)
	}
	if strings.Contains(out, "被拒绝的免修") {
		t.Fatalf("学生 %s 不应有被拒绝的免修，out=%q", student, out)
	}
	out, _, _ = runCLI(t, file, "show", student)
	wantLine := `免修 w1：要求 r1，依据 "` + basis + `"，状态：有效`
	if !strings.Contains(out, wantLine) {
		t.Fatalf("学生 %s 的免修历史应保留本人的 w1 与依据，out=%q", student, out)
	}
}

// assertCLIOtherWaiverAbsent 核对另一人的历史中不能出现申请人的同号免修：
// 仍为 0 学分、r1 未满足，show 中没有任何免修记录。
func assertCLIOtherWaiverAbsent(t *testing.T, file, student string) {
	t.Helper()
	out, _, code := runCLI(t, file, "check", student)
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "未满足要求：[r1]") ||
		strings.Contains(out, "有效免修") || strings.Contains(out, "被拒绝的免修") {
		t.Fatalf("学生 %s 不应因他人的同号申请获得免修，code=%d out=%q",
			student, code, out)
	}
	out, _, _ = runCLI(t, file, "show", student)
	if !strings.Contains(out, "免修历史：（无）") {
		t.Fatalf("学生 %s 的免修历史不能出现他人的申请，out=%q", student, out)
	}
}

// runCLISharedWaiverScenario 两种提交顺序共用的检查主体。
func runCLISharedWaiverScenario(t *testing.T, file, first, firstBasis, firstCredit string) {
	t.Helper()
	second, secondBasis, secondCredit := "s2", "外校修读证明", "3"
	secondCourse := "c2"
	firstCourse := "c1"
	if first == "s2" {
		second, secondBasis, secondCredit = "s1", "学科竞赛获奖", "4"
		secondCourse, firstCourse = "c1", "c2"
	}

	// 先提交的一份生效后，只能使申请人的要求满足。
	out, _, code := runCLI(t, file, "waiver", first, "r1", "w1", firstBasis)
	if code != 0 || !strings.Contains(out, "免修 w1 有效") {
		t.Fatalf("%s 首次提交 w1 应生效且退出码 0，code=%d out=%q", first, code, out)
	}
	assertCLIOwnerWaiver(t, file, first, firstCredit, firstCourse, firstBasis)

	// 另一人的要求仍未满足、学分为零，免修历史中不能出现前一人的申请。
	assertCLIOtherWaiverAbsent(t, file, second)

	// 另一人随后提交自己的同号申请：不能被当作重复提交、编号冲突或
	// 该要求已经免修，应作为本人的新申请正常生效（退出码 0）。
	out, _, code = runCLI(t, file, "waiver", second, "r1", "w1", secondBasis)
	if code != 0 || !strings.Contains(out, "免修 w1 有效") {
		t.Fatalf("%s 的同号 w1 应正常生效而非被判重复/冲突，code=%d out=%q",
			second, code, out)
	}

	// 两人的核对结果分别为各自课程学分，来源均指向本人名下的 w1，
	// 免修历史各保留本人的依据；同号要求仍能分清对应的课程。
	assertCLIOwnerWaiver(t, file, first, firstCredit, firstCourse, firstBasis)
	assertCLIOwnerWaiver(t, file, second, secondCredit, secondCourse, secondBasis)
	out, _, _ = runCLI(t, file, "show", first)
	if strings.Contains(out, secondBasis) {
		t.Fatalf("%s 的历史不能出现另一人的依据 %q，out=%q", first, secondBasis, out)
	}
	out, _, _ = runCLI(t, file, "show", second)
	if strings.Contains(out, firstBasis) {
		t.Fatalf("%s 的历史不能出现另一人的依据 %q，out=%q", second, firstBasis, out)
	}
}

// TestCLISharedWaiverIDsOwnership 两名学生共享要求编号 r1 与免修编号 w1，
// s1 先提交：归属、有效状态、学分来源与历史各归各。
func TestCLISharedWaiverIDsOwnership(t *testing.T) {
	file := tempRecordFile(t)
	setupCLISharedWaivers(t, file, false)
	runCLISharedWaiverScenario(t, file, "s1", "学科竞赛获奖", "4")
}

// TestCLISharedWaiverIDsOwnershipReversed 交换登记与提交顺序（s2 先），
// 归属结论必须一致。
func TestCLISharedWaiverIDsOwnershipReversed(t *testing.T) {
	file := tempRecordFile(t)
	setupCLISharedWaivers(t, file, true)
	runCLISharedWaiverScenario(t, file, "s2", "外校修读证明", "3")
}

// TestCLISharedWaiverSameStudentRulesKeptSeparate 两人各有一份同号 w1
// 生效后，同一学生名下的幂等、内容冲突与“已有有效免修”规则保持现有行为，
// 且任何拒绝都不能改动另一人的同号记录。
func TestCLISharedWaiverSameStudentRulesKeptSeparate(t *testing.T) {
	file := tempRecordFile(t)
	setupCLISharedWaivers(t, file, false)
	if _, _, code := runCLI(t, file, "waiver", "s1", "r1", "w1", "学科竞赛获奖"); code != 0 {
		t.Fatal("s1 首次申请 w1 应成功")
	}
	if _, _, code := runCLI(t, file, "waiver", "s2", "r1", "w1", "外校修读证明"); code != 0 {
		t.Fatal("s2 的同号 w1 应成功")
	}

	// 原编号、原要求、原依据再次提交：返回本人的原申请（退出码 0），
	// 历史条数和学分不增加，记录文件不发生变化。
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	out, _, code := runCLI(t, file, "waiver", "s1", "r1", "w1", "学科竞赛获奖")
	if code != 0 || !strings.Contains(out, "内容一致，返回原申请") ||
		!strings.Contains(out, "状态：有效") {
		t.Fatalf("原样重复提交应幂等返回原有效申请，code=%d out=%q", code, out)
	}
	after, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("幂等重复提交不应改写记录文件")
	}
	assertCLIOwnerWaiver(t, file, "s1", "4", "c1", "学科竞赛获奖")

	// 在原 w1 下改换依据：按内容冲突拒绝（退出码 1），不新增申请、不改文件，
	// 原依据与有效状态保留。
	_, errText, code := runCLI(t, file, "waiver", "s1", "r1", "w1", "改换后的依据")
	if code != 1 || !strings.Contains(errText, "w1") {
		t.Fatalf("同编号改换依据应按冲突拒绝并点名 w1，code=%d err=%q", code, errText)
	}
	after, err = os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("内容冲突被拒绝不应改写记录文件")
	}
	out, _, _ = runCLI(t, file, "show", "s1")
	if !strings.Contains(out, `免修 w1：要求 r1，依据 "学科竞赛获奖"，状态：有效`) {
		t.Fatalf("冲突拒绝后 s1 的原 w1 依据与有效状态应保留，out=%q", out)
	}
	if strings.Count(out, "免修 w1") != 1 {
		t.Fatalf("冲突拒绝不应新增申请，out=%q", out)
	}
	// 不能改动另一人的同号记录。
	assertCLIOwnerWaiver(t, file, "s2", "3", "c2", "外校修读证明")

	// 改用新免修编号 w2 再次取代已经免修的 r1：拒绝（退出码 1），
	// 并在提交者的历史中留下独立的拒绝记录。
	out, _, code = runCLI(t, file, "waiver", "s1", "r1", "w2", "再次申请的新依据")
	if code != 1 || !strings.Contains(out, "免修申请 w2 已拒绝") ||
		!strings.Contains(out, "该要求已有有效免修 w1") {
		t.Fatalf("新编号取代已有免修应拒绝并说明已有有效免修 w1，code=%d out=%q", code, out)
	}

	// 核对结果应列出这次申请的编号、目标要求、依据，以及已有有效免修的
	// 具体原因，不能只显示一个没有对应申请的失败提示。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("核对本身应成功，code=%d out=%q", code, out)
	}
	for _, want := range []string{
		"被拒绝的免修：",
		`免修 w2（要求 r1，依据 "再次申请的新依据"）`,
		"该要求已有有效免修 w1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("核对结果应包含 %q，out=%q", want, out)
		}
	}
	// 原有效申请继续作为学分来源，课程学分仍只计一次，要求仍为已满足。
	if !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为有效免修 w1") ||
		strings.Contains(out, "未满足要求：[") {
		t.Fatalf("原 w1 应继续作为唯一学分来源、r1 仍满足，out=%q", out)
	}
	out, _, _ = runCLI(t, file, "show", "s1")
	if !strings.Contains(out, `免修 w1：要求 r1，依据 "学科竞赛获奖"，状态：有效`) ||
		!strings.Contains(out, `免修 w2：要求 r1，依据 "再次申请的新依据"，状态：已拒绝`) {
		t.Fatalf("s1 历史应保留独立的 w1 有效与 w2 已拒绝两条，out=%q", out)
	}

	// 被拒绝的 w2 原样再次提交：沿用既有幂等行为返回原拒绝记录（退出码 0），
	// 不新增申请、不生效。
	out, _, code = runCLI(t, file, "waiver", "s1", "r1", "w2", "再次申请的新依据")
	if code != 0 || !strings.Contains(out, "内容一致，返回原申请") ||
		!strings.Contains(out, "状态：已拒绝") {
		t.Fatalf("w2 重复提交应幂等返回原拒绝记录，code=%d out=%q", code, out)
	}
	out, _, _ = runCLI(t, file, "check", "s1")
	if strings.Count(out, "免修 w2（要求 r1") != 1 || !strings.Contains(out, "总学分：4") {
		t.Fatalf("w2 重复提交不应新增拒绝记录或改变学分，out=%q", out)
	}

	// 另一人的免修历史、有效申请和核对结果始终保持原样。
	out, _, _ = runCLI(t, file, "show", "s2")
	if strings.Count(out, "免修 w1") != 1 || strings.Contains(out, "w2") ||
		strings.Contains(out, "学科竞赛获奖") {
		t.Fatalf("s2 的历史应始终只有本人的 w1 与本人依据，out=%q", out)
	}
	assertCLIOwnerWaiver(t, file, "s2", "3", "c2", "外校修读证明")
}
