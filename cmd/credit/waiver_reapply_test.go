package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件回归“免修已撤销后，用户换用新编号重新申请同一项要求”的完整命令行
// 行为。撤销只取消原申请对要求的满足作用，原申请（编号、依据、撤销原因）
// 仍留在免修历史中；这项要求当前没有有效免修，因此新编号申请必须能够正常
// 生效，不能因为历史里出现过免修就一直拒绝。同时保障：旧编号不会因重试
// 重新生效，新旧申请在历史中各自独立，学分始终只计一份。
//
// 公共场景：学生 s1 的要求 r1 指向 4 学分课程 c1，没有任何通过修读；
// 原免修 w1（依据“学科竞赛获奖”）已凭原因“材料无法核实”撤销。

// setupRevokedWaiver 登记学生、课程、要求，提交免修 w1 后撤销，并断言撤销后
// 核对为零学分、要求未满足、撤销原因与原依据保留在历史里。返回记录文件路径。
func setupRevokedWaiver(t *testing.T) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "records.json")
	for _, st := range [][]string{
		{"student", "s1"},
		{"course", "c1", "高等数学", "4"},
		{"req", "s1", "r1", "c1"},
		{"waiver", "s1", "r1", "w1", "学科竞赛获奖"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}
	out, errText, code := runCLI(t, file, "revoke-waiver", "s1", "w1", "材料无法核实")
	if code != 0 || !strings.Contains(out, "已撤销") ||
		!strings.Contains(out, `原依据 "学科竞赛获奖" 保留`) {
		t.Fatalf("撤销 w1 应成功并保留原依据，code=%d out=%q err=%q", code, out, errText)
	}

	// 撤销取消的是满足作用：没有通过修读，核对回到零学分、要求未满足。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "未满足要求：[r1]") ||
		strings.Contains(out, "来源为有效免修") {
		t.Fatalf("撤销后核对应为 0 学分且 r1 未满足，code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "已撤销免修：[w1]") {
		t.Fatalf("撤销后核对仍应列出已撤销免修 w1，out=%q", out)
	}
	// 原申请留在历史里：原要求、原依据与撤销原因都可查。
	out, _, code = runCLI(t, file, "show", "s1")
	if code != 0 ||
		!strings.Contains(out, `免修 w1：要求 r1，依据 "学科竞赛获奖"，状态：已撤销（材料无法核实）`) {
		t.Fatalf("撤销后历史应保留原要求、依据与撤销原因，code=%d out=%q", code, out)
	}
	return file
}

// TestCLINewWaiverIDAfterRevokeApproved 撤销后用该学生名下尚未使用过的编号
// w2 重新申请同一项要求：申请成功（退出码 0），核对应显示要求已满足、
// 总学分为 4 且来源指向新免修 w2；历史中旧申请仍是已撤销并保留原依据与
// 撤销原因，新申请另有自己的编号、依据与有效状态，两份申请并存也只计
// 4 学分，绝不累加成 8。
func TestCLINewWaiverIDAfterRevokeApproved(t *testing.T) {
	file := setupRevokedWaiver(t)

	// 新编号、含实际文字的依据：历史中有过免修不构成拒绝理由。
	out, errText, code := runCLI(t, file, "waiver", "s1", "r1", "w2", "外校同层次课程")
	if code != 0 || !strings.Contains(out, "免修 w2 有效") {
		t.Fatalf("撤销后换用新编号申请应成功且退出码 0，code=%d out=%q err=%q",
			code, out, errText)
	}

	// 核对：要求已满足、总学分恰为 4、来源是新免修 w2。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("新免修生效后核对应成功，code=%d out=%q", code, out)
	}
	for _, want := range []string{
		"总学分：4",
		"要求 r1（课程 c1《高等数学》，4 学分）：已满足",
		"来源为有效免修 w2",
		"已撤销免修：[w1]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("核对结果应包含 %q，out=%q", want, out)
		}
	}
	if strings.Contains(out, "总学分：8") || strings.Contains(out, "未满足要求：[r1]") {
		t.Fatalf("两份历史申请不能计入 8 学分，r1 也不应再未满足，out=%q", out)
	}
	// 来源只能指向新申请：已撤销的 w1 不能被描述成来源。
	if strings.Contains(out, "来源为有效免修 w1") {
		t.Fatalf("已撤销的 w1 不能成为学分来源，out=%q", out)
	}

	// 历史：旧申请保持已撤销（原要求、依据、撤销原因不变），新申请独立成行，
	// 两者各出现一次，新申请不覆盖旧申请。
	out, _, code = runCLI(t, file, "show", "s1")
	if code != 0 {
		t.Fatalf("show 应成功，code=%d out=%q", code, out)
	}
	for _, want := range []string{
		`免修 w1：要求 r1，依据 "学科竞赛获奖"，状态：已撤销（材料无法核实）`,
		`免修 w2：要求 r1，依据 "外校同层次课程"，状态：有效`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("免修历史应完整保留 %q，out=%q", want, out)
		}
	}
	for _, line := range []string{"免修 w1：要求 r1", "免修 w2：要求 r1"} {
		if strings.Count(out, line) != 1 {
			t.Fatalf("show 中 %q 应恰好出现一次（不覆盖、不重复），out=%q", line, out)
		}
	}
}

// TestCLIOldWaiverIDStaysRevokedOnResubmit 旧编号不能借重新申请的机会恢复
// 生效：用旧编号 w1、原要求与原依据再次提交，无论发生在新申请 w2 生效之前
// 还是之后，都成功返回原申请（退出码 0）并明确显示已撤销，不新增历史、不
// 重新授予学分；w2 有效时旧编号的重试也不能替换或撤销它。
func TestCLIOldWaiverIDStaysRevokedOnResubmit(t *testing.T) {
	file := setupRevokedWaiver(t)

	// 新申请出现之前：旧编号同内容重试，幂等返回原申请，状态仍是已撤销。
	out, _, code := runCLI(t, file, "waiver", "s1", "r1", "w1", "学科竞赛获奖")
	if code != 0 || !strings.Contains(out, "返回原申请") ||
		!strings.Contains(out, "状态：已撤销") {
		t.Fatalf("撤销后旧编号重试应返回原申请并显示已撤销，code=%d out=%q", code, out)
	}
	// 不重新授予学分：核对仍是 0 学分、要求未满足。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "未满足要求：[r1]") {
		t.Fatalf("旧编号重试不能重新授予学分，code=%d out=%q", code, out)
	}
	// 不新增历史：w1 在历史中仍只有一行。
	if out, _, _ := runCLI(t, file, "show", "s1"); strings.Count(out, "免修 w1：要求 r1") != 1 {
		t.Fatalf("旧编号重试不应新增免修历史，out=%q", out)
	}

	// 新编号 w2 申请生效。
	if out, _, code := runCLI(t, file, "waiver", "s1", "r1", "w2", "外校同层次课程"); code != 0 ||
		!strings.Contains(out, "有效") {
		t.Fatalf("新编号申请应生效，code=%d out=%q", code, out)
	}

	// 新申请已有效之后：旧编号同内容重试仍只返回已撤销的原申请。
	out, _, code = runCLI(t, file, "waiver", "s1", "r1", "w1", "学科竞赛获奖")
	if code != 0 || !strings.Contains(out, "返回原申请") ||
		!strings.Contains(out, "状态：已撤销") {
		t.Fatalf("新申请生效后旧编号重试仍应显示已撤销，code=%d out=%q", code, out)
	}
	// 核对仍以新申请说明来源，旧申请的重试没有替换或撤销 w2。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为有效免修 w2") ||
		!strings.Contains(out, "已撤销免修：[w1]") {
		t.Fatalf("旧编号重试后核对仍应以 w2 为来源、w1 保持已撤销，code=%d out=%q", code, out)
	}
	// 历史仍是各一行：旧申请未被改写，新申请仍是有效。
	out, _, _ = runCLI(t, file, "show", "s1")
	if strings.Count(out, "免修 w1：要求 r1") != 1 ||
		strings.Count(out, "免修 w2：要求 r1") != 1 ||
		!strings.Contains(out, `免修 w2：要求 r1，依据 "外校同层次课程"，状态：有效`) {
		t.Fatalf("旧编号重试不应改动新旧两份申请的历史与状态，out=%q", out)
	}
}

// TestCLIOldWaiverIDWithDifferentBasisRejected 旧编号携带不同依据再次提交时，
// 沿用同编号换内容的冲突拒绝规则（退出码 1），不能把它当作一次新的申请；
// 原已撤销申请与当前核对结果保持不变。
func TestCLIOldWaiverIDWithDifferentBasisRejected(t *testing.T) {
	file := setupRevokedWaiver(t)
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}

	_, errText, code := runCLI(t, file, "waiver", "s1", "r1", "w1", "另一份依据")
	if code != 1 {
		t.Fatalf("旧编号换依据应按内容冲突拒绝且退出码 1，code=%d err=%q", code, errText)
	}
	if !strings.Contains(errText, "w1") || !strings.Contains(errText, "已存在") {
		t.Fatalf("冲突拒绝应点名旧编号 w1 已存在，err=%q", errText)
	}

	// 拒绝不改动任何记录：文件逐字节保留，核对与历史与之前一致。
	after, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("冲突拒绝不应改写记录文件\nbefore=%s\nafter=%s", before, after)
	}
	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "未满足要求：[r1]") {
		t.Fatalf("冲突拒绝后核对应保持 0 学分未满足，code=%d out=%q", code, out)
	}
	if out, _, _ := runCLI(t, file, "show", "s1"); strings.Count(out, "免修 w1：要求 r1") != 1 ||
		!strings.Contains(out, "状态：已撤销（材料无法核实）") ||
		strings.Contains(out, "另一份依据") {
		t.Fatalf("冲突拒绝不应把不同依据写成新申请或改写原申请，out=%q", out)
	}
}

// TestCLISecondNewWaiverRejectedWhileValidExists 新申请 w2 已经有效时，再用
// 另一个新编号 w3 申请同一要求：以退出码 1 拒绝，这次申请及指明现有有效
// 免修编号的原因保存在该学生的拒绝历史中；原有效申请继续满足要求，总学分
// 仍为 4。
func TestCLISecondNewWaiverRejectedWhileValidExists(t *testing.T) {
	file := setupRevokedWaiver(t)
	if out, _, code := runCLI(t, file, "waiver", "s1", "r1", "w2", "外校同层次课程"); code != 0 ||
		!strings.Contains(out, "有效") {
		t.Fatalf("新编号 w2 申请应生效，code=%d out=%q", code, out)
	}

	// 另一个新编号 w3：拒绝（退出码 1），反馈中说明已有有效免修 w2。
	out, _, code := runCLI(t, file, "waiver", "s1", "r1", "w3", "另一份竞赛获奖")
	if code != 1 || !strings.Contains(out, "已拒绝") ||
		!strings.Contains(out, "该要求已有有效免修 w2") {
		t.Fatalf("已有有效免修时另一新编号应拒绝并点名 w2，code=%d out=%q", code, out)
	}

	// 拒绝历史持久保存：核对列出 w3 及指明 w2 的原因；原有效申请继续满足
	// 要求，总学分仍为 4，来源仍是 w2。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("拒绝 w3 后核对应成功，code=%d out=%q", code, out)
	}
	for _, want := range []string{
		"总学分：4",
		"来源为有效免修 w2",
		"被拒绝的免修：",
		`免修 w3（要求 r1，依据 "另一份竞赛获奖"）：该要求已有有效免修 w2`,
		"已撤销免修：[w1]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("核对结果应包含 %q，out=%q", want, out)
		}
	}
	if strings.Contains(out, "总学分：8") {
		t.Fatalf("三份历史申请不能计入 8 学分，out=%q", out)
	}

	// 历史查询：三份申请各自独立成行，状态与原因互不覆盖。
	out, _, code = runCLI(t, file, "show", "s1")
	if code != 0 {
		t.Fatalf("show 应成功，code=%d out=%q", code, out)
	}
	for _, want := range []string{
		`免修 w1：要求 r1，依据 "学科竞赛获奖"，状态：已撤销（材料无法核实）`,
		`免修 w2：要求 r1，依据 "外校同层次课程"，状态：有效`,
		`免修 w3：要求 r1，依据 "另一份竞赛获奖"，状态：已拒绝（该要求已有有效免修 w2）`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("免修历史应完整保留 %q，out=%q", want, out)
		}
	}
	for _, line := range []string{"免修 w1：要求 r1", "免修 w2：要求 r1", "免修 w3：要求 r1"} {
		if strings.Count(out, line) != 1 {
			t.Fatalf("show 中 %q 应恰好出现一次，out=%q", line, out)
		}
	}
}
