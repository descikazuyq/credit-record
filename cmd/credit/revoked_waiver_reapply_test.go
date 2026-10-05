package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件回归“一份免修撤销后，用户换用新编号重新申请同一项要求”的行为。
// 撤销取消的只是原申请对要求的满足作用，原申请仍留在历史里；新申请能否
// 生效只看这项要求当前有没有有效免修，不能因为历史里出现过免修就一直拒绝。
//
// 每条断言都重新打开进程访问同一记录文件，因此同时覆盖落盘与重新加载后
// 申请反馈、核对结果与免修历史之间的一致性，不引入新的审批条件，也不改变
// 已有退出码：
//   - 原免修撤销、无通过修读：核对 0 学分、要求未满足；用该学生名下尚未
//     使用过的新编号与含实际文字的依据申请，退出码 0、正常生效；核对随之
//     变为已满足、总学分 4、来源指向新编号；旧申请仍是已撤销，原要求、
//     原依据与撤销原因完整保留，两份历史不能互相覆盖，学分不能因两份历史
//     记成 8；
//   - 旧编号按原要求、原依据重试：新申请之前与新申请已经有效之后都只返回
//     原申请、明确显示已撤销，不新增历史、不重新授予学分，更不能替换或
//     撤销已经有效的新申请；旧编号携带不同依据时沿用内容冲突拒绝规则，
//     不能被当作一次新申请；
//   - 新申请已经有效时，再用另一个新编号申请同一要求：退出码 1 拒绝，
//     申请内容与“现有有效免修编号”的原因保存在该学生的拒绝历史中，
//     原有效申请继续满足要求，总学分仍为 4。

const (
	rwrOldBasis   = "学科竞赛获奖"
	rwrNewBasis   = "外校同层次课程成绩单"
	rwrThirdBasis = "高水平运动队证明"
	rwrRevokeRsn  = "获奖材料无法核实"
)

// setupRevokedWaiverFile 建立主情形并返回记录文件路径：已登记学生 s1 有一项
// 指向 4 学分课程 c1 的要求 r1，要求没有任何修读；免修 w1 曾有效，随后被
// 撤销，原依据与撤销原因均保留。构造完成时核对必须为 0 学分、要求未满足。
func setupRevokedWaiverFile(t *testing.T) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "records.json")
	for _, st := range [][]string{
		{"student", "s1"},
		{"course", "c1", "高等数学", "4"},
		{"req", "s1", "r1", "c1"},
		{"waiver", "s1", "r1", "w1", rwrOldBasis},
		{"revoke-waiver", "s1", "w1", rwrRevokeRsn},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("准备步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}

	// 撤销只取消满足作用：要求未通过修读，核对必须为 0 学分、未满足，
	// 并列出已撤销的 w1。
	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "未满足要求：[r1]") ||
		!strings.Contains(out, "已撤销免修：[w1]") ||
		strings.Contains(out, "来源为有效免修") {
		t.Fatalf("撤销后且无通过修读时应为 0 学分、r1 未满足，code=%d out=%q", code, out)
	}
	return file
}

// assertRevokedW1History 断言旧申请 w1 在历史中仍为已撤销，原要求、原依据
// 与撤销原因完整保留。
func assertRevokedW1History(t *testing.T, file string) {
	t.Helper()
	out, _, code := runCLI(t, file, "show", "s1")
	if code != 0 {
		t.Fatalf("show 应成功，code=%d out=%q", code, out)
	}
	want := `免修 w1：要求 r1，依据 "` + rwrOldBasis + `"，状态：已撤销（` + rwrRevokeRsn + "）"
	if !strings.Contains(out, want) {
		t.Fatalf("旧申请应保留原要求、原依据与撤销原因且仍为已撤销，out=%q", out)
	}
}

// assertNewWaiverSatisfies 核对：r1 已满足、总学分 4、唯一来源是新编号，
// 不存在 8 学分或两份来源；旧 w1 仍作为已撤销列出。
func assertNewWaiverSatisfies(t *testing.T, file, newID string) {
	t.Helper()
	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("核对应成功，code=%d out=%q", code, out)
	}
	for _, want := range []string{
		"总学分：4",
		"要求 r1（课程 c1《高等数学》，4 学分）：已满足",
		"来源为有效免修 " + newID,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("核对结果应包含 %q，out=%q", want, out)
		}
	}
	if strings.Contains(out, "未满足要求：[") {
		t.Fatalf("r1 应由新免修满足，不应再列为未满足，out=%q", out)
	}
	if strings.Contains(out, "总学分：8") || strings.Contains(out, "来源为有效免修 w1") {
		t.Fatalf("已撤销旧申请不能重复授予学分或成为来源，out=%q", out)
	}
	// 来源行全份报告只能出现一次：两份历史不能各计一份 4 学分。
	if strings.Count(out, "来源为有效免修 "+newID) != 1 {
		t.Fatalf("新免修作为来源应只计一次，out=%q", out)
	}
	if !strings.Contains(out, "已撤销免修：[w1]") {
		t.Fatalf("核对仍应列出已撤销的旧申请 w1，out=%q", out)
	}
}

// assertNoNewHistory 断言免修历史中 w1 行与给定新编号行各只出现一次，
// 重试没有新增第三条历史。
func assertNoNewHistory(t *testing.T, file, newID string) {
	t.Helper()
	out, _, code := runCLI(t, file, "show", "s1")
	if code != 0 {
		t.Fatalf("show 应成功，code=%d out=%q", code, out)
	}
	if strings.Count(out, "免修 w1：要求 r1") != 1 {
		t.Fatalf("旧申请重试不应新增历史，w1 行应恰好一次，out=%q", out)
	}
	if newID != "" && strings.Count(out, "免修 "+newID+"：要求 r1") != 1 {
		t.Fatalf("新申请 %s 应恰有一条历史，out=%q", newID, out)
	}
}

// TestCLIRevokedWaiverNewIDAppliesAndKeepsOldHistory 主情形：原免修已撤销、
// 要求未通过修读，核对为 0；用该学生名下未使用过的新编号与含实际文字的
// 依据申请应成功（退出码 0），随后核对为已满足、总学分 4、来源指向新编号；
// 旧申请仍是已撤销并保留原要求、依据与撤销原因，两份申请各有独立编号、
// 依据与状态，不能互相覆盖，也不能因历史中有两份申请而计成 8 学分。
func TestCLIRevokedWaiverNewIDAppliesAndKeepsOldHistory(t *testing.T) {
	file := setupRevokedWaiverFile(t)

	// 新编号、新依据：申请成功，退出码 0。
	out, _, code := runCLI(t, file, "waiver", "s1", "r1", "w2", rwrNewBasis)
	if code != 0 {
		t.Fatalf("原免修撤销后用新编号申请应成功（退出码 0），code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "免修 w2 有效") ||
		!strings.Contains(out, "要求 r1") || !strings.Contains(out, rwrNewBasis) {
		t.Fatalf("成功反馈应说明新免修 w2 有效并给出要求与实际依据，out=%q", out)
	}

	assertNewWaiverSatisfies(t, file, "w2")

	// 历史查询：旧申请仍是已撤销（原要求、原依据、撤销原因），新申请另有
	// 自己的编号、依据与有效状态；两份不能互相覆盖。
	show, _, code := runCLI(t, file, "show", "s1")
	if code != 0 {
		t.Fatalf("show 应成功，code=%d out=%q", code, show)
	}
	for _, want := range []string{
		`免修 w1：要求 r1，依据 "` + rwrOldBasis + `"，状态：已撤销（` + rwrRevokeRsn + "）",
		`免修 w2：要求 r1，依据 "` + rwrNewBasis + `"，状态：有效`,
	} {
		if !strings.Contains(show, want) {
			t.Fatalf("免修历史应同时保留 %q，out=%q", want, show)
		}
	}
	if strings.Count(show, "免修 w1：要求 r1") != 1 ||
		strings.Count(show, "免修 w2：要求 r1") != 1 {
		t.Fatalf("新旧两份申请应各占一条历史、互不覆盖，out=%q", show)
	}
}

// TestCLIOldRevokedWaiverRetryBeforeNewApplication 新申请之前用旧编号、原要求、
// 原依据再次提交：返回原申请并明确显示已撤销，退出码 0，不新增历史、不授予
// 学分；之后新编号申请仍可正常生效。
func TestCLIOldRevokedWaiverRetryBeforeNewApplication(t *testing.T) {
	file := setupRevokedWaiverFile(t)
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}

	// 原样重试已撤销申请：幂等返回原申请，状态明确为已撤销。
	for i := 0; i < 2; i++ {
		out, _, code := runCLI(t, file, "waiver", "s1", "r1", "w1", rwrOldBasis)
		if code != 0 {
			t.Fatalf("第 %d 次重试已撤销申请应返回原申请且退出码 0，code=%d out=%q",
				i+1, code, out)
		}
		if !strings.Contains(out, "免修 w1 已提交过且内容一致，返回原申请") ||
			!strings.Contains(out, "状态：已撤销") ||
			strings.Contains(out, "状态：有效") {
			t.Fatalf("重试反馈应明确显示原申请已撤销而非复活，out=%q", out)
		}
	}

	// 幂等重试不产生变更：记录文件字节不变。
	after, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("重试已撤销申请不应改写记录文件\nbefore=%s\nafter=%s", before, after)
	}

	// 不新增历史、不授予学分。
	assertNoNewHistory(t, file, "")
	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "未满足要求：[r1]") {
		t.Fatalf("重试已撤销申请不能重新授予学分，code=%d out=%q", code, out)
	}
	assertRevokedW1History(t, file)

	// 旧申请不复活不影响随后用新编号正常申请。
	out, _, code = runCLI(t, file, "waiver", "s1", "r1", "w2", rwrNewBasis)
	if code != 0 || !strings.Contains(out, "免修 w2 有效") {
		t.Fatalf("旧编号重试后新编号申请仍应生效，code=%d out=%q", code, out)
	}
	assertNewWaiverSatisfies(t, file, "w2")
	assertRevokedW1History(t, file)
}

// TestCLIOldRevokedWaiverRetryAfterNewApplication 新申请已经有效之后，旧编号
// 按原要求、原依据重试仍只返回已撤销的原申请：不新增历史、不重新授予学分，
// 也不能替换或撤销已经有效的新申请；核对继续以新申请说明来源，总学分仍为 4。
func TestCLIOldRevokedWaiverRetryAfterNewApplication(t *testing.T) {
	file := setupRevokedWaiverFile(t)
	if out, _, code := runCLI(t, file, "waiver", "s1", "r1", "w2", rwrNewBasis); code != 0 {
		t.Fatalf("新编号申请应成功，code=%d out=%q", code, out)
	}
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}

	out, _, code := runCLI(t, file, "waiver", "s1", "r1", "w1", rwrOldBasis)
	if code != 0 {
		t.Fatalf("旧编号原样重试应返回原申请且退出码 0，code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "返回原申请") || !strings.Contains(out, "状态：已撤销") ||
		strings.Contains(out, "状态：有效") {
		t.Fatalf("重试反馈应明确显示旧申请仍已撤销，不能复活或替换新申请，out=%q", out)
	}

	// 幂等重试不产生变更、不新增历史。
	after, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("旧编号重试不应改写记录文件")
	}
	assertNoNewHistory(t, file, "w2")

	// 核对仍以新申请 w2 说明来源，总学分仍为 4。
	assertNewWaiverSatisfies(t, file, "w2")
	assertRevokedW1History(t, file)
}

// TestCLIOldRevokedWaiverRetryWithDifferentBasisConflicts 旧编号携带不同依据
// 重试（无论在新申请之前还是之后）：沿用内容冲突拒绝规则，退出码 1，不能
// 当作一次新申请；原申请依据与状态、已有有效申请与核对结果都保持不变。
func TestCLIOldRevokedWaiverRetryWithDifferentBasisConflicts(t *testing.T) {
	t.Run("新申请之前换依据", func(t *testing.T) {
		file := setupRevokedWaiverFile(t)
		before, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}

		_, errText, code := runCLI(t, file, "waiver", "s1", "r1", "w1", "换了一份依据")
		if code != 1 {
			t.Fatalf("旧编号换依据应按内容冲突拒绝（退出码 1），code=%d err=%q", code, errText)
		}
		if !strings.Contains(errText, "w1") || !strings.Contains(errText, "提交内容不同") {
			t.Fatalf("冲突错误应点名旧编号并说明内容不同，err=%q", errText)
		}

		after, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if string(before) != string(after) {
			t.Fatal("内容冲突被拒绝不应改写记录文件")
		}
		// 原依据与已撤销状态保留，不新增历史；核对仍为 0 学分、未满足。
		assertRevokedW1History(t, file)
		assertNoNewHistory(t, file, "")
		out, _, _ := runCLI(t, file, "check", "s1")
		if !strings.Contains(out, "总学分：0") || !strings.Contains(out, "未满足要求：[r1]") {
			t.Fatalf("冲突拒绝后应仍为 0 学分、r1 未满足，out=%q", out)
		}

		// 冲突不影响随后用新编号正常申请。
		if out, _, code := runCLI(t, file, "waiver", "s1", "r1", "w2", rwrNewBasis); code != 0 {
			t.Fatalf("冲突后新编号申请仍应成功，code=%d out=%q", code, out)
		}
		assertNewWaiverSatisfies(t, file, "w2")
	})

	t.Run("新申请有效之后换依据", func(t *testing.T) {
		file := setupRevokedWaiverFile(t)
		if _, _, code := runCLI(t, file, "waiver", "s1", "r1", "w2", rwrNewBasis); code != 0 {
			t.Fatal("新编号申请应成功")
		}
		before, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}

		_, errText, code := runCLI(t, file, "waiver", "s1", "r1", "w1", "另一份不同的依据")
		if code != 1 || !strings.Contains(errText, "w1") ||
			!strings.Contains(errText, "提交内容不同") {
			t.Fatalf("有效后旧编号换依据仍应按冲突拒绝并点名 w1，code=%d err=%q", code, errText)
		}
		after, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if string(before) != string(after) {
			t.Fatal("内容冲突被拒绝不应改写记录文件")
		}
		// 旧申请依据与撤销状态不变；新申请继续满足要求、总学分仍为 4。
		assertRevokedW1History(t, file)
		assertNewWaiverSatisfies(t, file, "w2")
		assertNoNewHistory(t, file, "w2")
	})
}

// TestCLIAnotherNewIDRejectedAfterNewWaiverValid 新申请 w2 已经有效时，再用
// 另一个新编号 w3 申请同一要求：退出码 1 拒绝，这次申请及指明现有有效免修
// 编号 w2 的原因保存在该学生的拒绝历史中；原有效申请继续满足要求，总学分
// 仍为 4。对 w3 的原样重试只返回已拒绝记录，不新增历史、不改变学分。
func TestCLIAnotherNewIDRejectedAfterNewWaiverValid(t *testing.T) {
	file := setupRevokedWaiverFile(t)
	if out, _, code := runCLI(t, file, "waiver", "s1", "r1", "w2", rwrNewBasis); code != 0 {
		t.Fatalf("新编号 w2 申请应成功，code=%d out=%q", code, out)
	}

	// 再用另一个新编号申请同一要求：拒绝（退出码 1），原因点名现有有效 w2。
	out, _, code := runCLI(t, file, "waiver", "s1", "r1", "w3", rwrThirdBasis)
	if code != 1 {
		t.Fatalf("已有有效免修时新编号申请应拒绝（退出码 1），code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "免修申请 w3 已拒绝") ||
		!strings.Contains(out, "该要求已有有效免修 w2") {
		t.Fatalf("拒绝反馈应说明 w3 已入历史并指出现有有效免修 w2，out=%q", out)
	}

	// 核对：w3 与其依据、拒绝原因（点名 w2）出现在被拒绝列表；原 w2 继续
	// 作为唯一来源，总学分仍为 4，旧 w1 仍已撤销。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("核对本身应成功，code=%d out=%q", code, out)
	}
	for _, want := range []string{
		"总学分：4",
		"来源为有效免修 w2",
		"被拒绝的免修：",
		`免修 w3（要求 r1，依据 "` + rwrThirdBasis + `"）：该要求已有有效免修 w2`,
		"已撤销免修：[w1]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("核对结果应包含 %q，out=%q", want, out)
		}
	}
	if strings.Contains(out, "未满足要求：[") {
		t.Fatalf("原有效申请继续满足 r1，不应列为未满足，out=%q", out)
	}

	// show：三份历史各占一条，状态分别为已撤销/有效/已拒绝。
	show, _, code := runCLI(t, file, "show", "s1")
	if code != 0 {
		t.Fatalf("show 应成功，code=%d out=%q", code, show)
	}
	for _, want := range []string{
		`免修 w1：要求 r1，依据 "` + rwrOldBasis + `"，状态：已撤销（` + rwrRevokeRsn + "）",
		`免修 w2：要求 r1，依据 "` + rwrNewBasis + `"，状态：有效`,
		`免修 w3：要求 r1，依据 "` + rwrThirdBasis + `"，状态：已拒绝（该要求已有有效免修 w2）`,
	} {
		if !strings.Contains(show, want) {
			t.Fatalf("免修历史应保留 %q，out=%q", want, show)
		}
	}

	// w3 原样重试：幂等返回已拒绝记录，不新增历史、不改变学分与来源。
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	out, _, code = runCLI(t, file, "waiver", "s1", "r1", "w3", rwrThirdBasis)
	if code != 0 || !strings.Contains(out, "返回原申请") ||
		!strings.Contains(out, "状态：已拒绝") {
		t.Fatalf("w3 原样重试应幂等返回已拒绝记录，code=%d out=%q", code, out)
	}
	after, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("w3 原样重试不应改写记录文件")
	}
	out, _, _ = runCLI(t, file, "check", "s1")
	if strings.Count(out, "免修 w3（要求 r1") != 1 ||
		!strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为有效免修 w2") {
		t.Fatalf("w3 重试不应新增拒绝记录或改变学分来源，out=%q", out)
	}
}
