package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件从命令行入口回归“同一学生的同一要求只能有一份有效免修”在读取
// 既有记录文件时的表现。互相重复取代的两份有效免修无法通过正常命令产生
// （提交时第二份就会被拒绝），因此这里直接把完整、可解析的记录写入文件，
// 再通过现有命令（check/show 及写入类命令）访问它，每条命令都重新打开
// 记录文件。

// marshalPreexisting 按记录文件磁盘格式序列化一份预先存在的记录。
func marshalPreexisting(t *testing.T, v map[string]any) []byte {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("序列化预置记录失败：%v", err)
	}
	return append(b, '\n')
}

// writePreexisting 写入预置记录，返回文件路径与原始字节。
func writePreexisting(t *testing.T, v map[string]any) (string, []byte) {
	t.Helper()
	raw := marshalPreexisting(t, v)
	file := filepath.Join(t.TempDir(), "records.json")
	if err := os.WriteFile(file, raw, 0o600); err != nil {
		t.Fatalf("写入预置记录失败：%v", err)
	}
	return file, raw
}

func diskCourse(id, name string, credit int) map[string]any {
	return map[string]any{"id": id, "name": name, "credit": credit, "open": true}
}

func diskWaiver(id, student, req, basis, status, reason string) map[string]any {
	w := map[string]any{"id": id, "student": student, "req": req,
		"basis": basis, "status": status}
	if reason != "" {
		w["reason"] = reason
	}
	return w
}

// conflictRecord 构造一份除免修外完全合法的预置记录：学生 s1 的要求 r1
// 指向 4 学分的 c1。withPass 时另有一次通过修读 e1；waivers 给出免修历史。
func conflictRecord(withPass bool, waivers []any) map[string]any {
	d := map[string]any{
		"version": 1,
		"courses": []any{diskCourse("c1", "高等数学", 4)},
		"students": []any{
			map[string]any{"id": "s1"},
		},
		"requirements": []any{
			map[string]any{"id": "r1", "student": "s1", "course": "c1"},
		},
		"enrollments":   []any{},
		"waivers":       waivers,
		"nextResultSeq": 0,
	}
	if withPass {
		d["enrollments"] = []any{
			map[string]any{"student": "s1", "id": "e1", "req": "r1",
				"term": "2024春", "result": "passed", "resultSeq": 1},
		}
		d["nextResultSeq"] = 1
	}
	return d
}

// assertConflictFilePreserved 用三类现有命令访问冲突文件，都必须沿用文件
// 内容损坏的退出码 2：不输出学分核对结果、历史或登记成功信息；原文件逐字节保留。
func assertConflictFilePreserved(t *testing.T, file string, raw []byte, label string) {
	t.Helper()

	// 只读核对：不能返回基于冲突记录的学分结果。
	out, errText, code := runCLI(t, file, "check", "s1")
	if code != exitFile {
		t.Fatalf("%s：check 遇免修冲突应退出码 %d，code=%d out=%q err=%q",
			label, exitFile, code, out, errText)
	}
	if out != "" {
		t.Fatalf("%s：check 拒绝冲突文件时不应有业务输出，out=%q", label, out)
	}
	for _, want := range []string{"内容损坏", file, "s1", "r1", "w1", "w2"} {
		if !strings.Contains(errText, want) {
			t.Fatalf("%s：check 的错误应包含 %q（文件/学生/要求/冲突编号），err=%q",
				label, want, errText)
		}
	}
	if strings.Contains(errText, "未做任何修改") == false {
		t.Fatalf("%s：错误应说明未做任何修改，err=%q", label, errText)
	}

	// 历史查看同样不得打开这份记录。
	out, errText, code = runCLI(t, file, "show", "s1")
	if code != exitFile || out != "" {
		t.Fatalf("%s：show 遇免修冲突应退出码 %d 且无业务输出，code=%d out=%q err=%q",
			label, exitFile, code, out, errText)
	}
	if !strings.Contains(errText, "内容损坏") || !strings.Contains(errText, "s1") ||
		!strings.Contains(errText, "r1") || !strings.Contains(errText, "w1") ||
		!strings.Contains(errText, "w2") {
		t.Fatalf("%s：show 的错误应点名学生、要求与两个冲突编号，err=%q",
			label, errText)
	}

	// 写入类命令（新免修申请）：Load 阶段就应失败，不能报告申请结果，
	// 更不能把冲突文件覆盖成一份看似正常的新记录。区分点在于：这必须是
	// 读取阶段的“内容损坏/未做任何修改”，而不是申请被业务拒绝（退出码 1
	// 且提示“该要求已有有效免修”）。
	out, errText, code = runCLI(t, file, "waiver", "s1", "r1", "w3", "新的依据")
	if code != exitFile {
		t.Fatalf("%s：写入类命令遇免修冲突应退出码 %d，code=%d out=%q err=%q",
			label, exitFile, code, out, errText)
	}
	if out != "" {
		t.Fatalf("%s：拒绝读取时不应继续办理免修申请，out=%q", label, out)
	}
	if !strings.Contains(errText, "内容损坏") || !strings.Contains(errText, "未做任何修改") {
		t.Fatalf("%s：应停在读取阶段的文件损坏报错，而非业务拒绝，err=%q", label, errText)
	}

	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(raw) {
		t.Fatalf("%s：原文件内容必须完整保留\nwant=%q\n got=%q", label, raw, got)
	}
}

// TestCLILoadRejectsDuplicateApprovedWaivers 文件即使完整可解析、学生课程
// 要求都存在、两份申请都有非空依据，只要同一学生的一项要求下有两份编号不同、
// 状态均为有效的免修，通过现有命令访问时就必须沿用文件损坏退出码 2：
// 不核对、不登记、不改写，错误点名文件、学生、要求与两个免修编号。
// 两份有效免修的先后顺序，以及目标要求是否另有通过修读，都不能改变结论。
func TestCLILoadRejectsDuplicateApprovedWaivers(t *testing.T) {
	w1 := diskWaiver("w1", "s1", "r1", "学科竞赛获奖", "approved", "")
	w2 := diskWaiver("w2", "s1", "r1", "外校同层次课程", "approved", "")

	cases := map[string]struct {
		withPass bool
		waivers  []any
	}{
		"第二份有效免修排在后面":         {false, []any{w1, w2}},
		"第二份有效免修排在前面":         {false, []any{w2, w1}},
		"另有通过修读排在免修之前也不能掩盖冲突": {true, []any{w1, w2}},
		"另有通过修读且有效免修顺序倒置":     {true, []any{w2, w1}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			file, raw := writePreexisting(t, conflictRecord(tc.withPass, tc.waivers))
			assertConflictFilePreserved(t, file, raw, name)

			// 再次访问仍必须失败：第一次拒绝不能把文件“修正”成可打开状态。
			if out, _, code := runCLI(t, file, "check", "s1"); code != exitFile || out != "" {
				t.Fatalf("冲突文件再次访问仍应拒绝，code=%d out=%q", code, out)
			}
			if got, err := os.ReadFile(file); err != nil || string(got) != string(raw) {
				t.Fatalf("重复访问后原文件仍应完整保留，err=%v", err)
			}
		})
	}
}

// TestCLILoadMixedWaiverHistoryKeepsSingleSource 同一学生的同一要求可以
// 同时保留一份有效免修、一份已拒绝申请和一份已撤销申请：记录应正常打开，
// 核对只以有效免修说明来源、课程学分只计一次；历史中各份申请的编号、依据、
// 状态与原有拒绝/撤销原因都对应原记录，不能为去重丢历史，也不能让失效
// 申请重新参与满足要求。失效历史排在有效申请之前或之后结论相同。
func TestCLILoadMixedWaiverHistoryKeepsSingleSource(t *testing.T) {
	wok := diskWaiver("wok", "s1", "r1", "学科竞赛获奖", "approved", "")
	wrej := diskWaiver("wrej", "s1", "r1", "", "rejected", "免修依据为空")
	wrev := diskWaiver("wrev", "s1", "r1", "已过期的外校证明", "revoked", "材料无法核实")

	cases := map[string][]any{
		"失效历史排在有效申请之后": {wok, wrej, wrev},
		"失效历史排在有效申请之前": {wrev, wrej, wok},
	}
	for name, waivers := range cases {
		t.Run(name, func(t *testing.T) {
			file, raw := writePreexisting(t, conflictRecord(false, waivers))

			out, errText, code := runCLI(t, file, "check", "s1")
			if code != 0 {
				t.Fatalf("%s：合法混合历史应正常核对，code=%d out=%q err=%q",
					name, code, out, errText)
			}
			// 只以那份有效免修说明来源，4 学分只计一次。
			for _, want := range []string{
				"总学分：4",
				"要求 r1（课程 c1《高等数学》，4 学分）：已满足，来源为有效免修 wok",
				"未满足要求：（无）",
				"被拒绝的免修：",
				`免修 wrej（要求 r1，依据 ""）：免修依据为空`,
				"已撤销免修：[wrev]",
			} {
				if !strings.Contains(out, want) {
					t.Fatalf("%s：核对结果应包含 %q，out=%q", name, want, out)
				}
			}
			if strings.Count(out, "总学分：4") != 1 {
				t.Fatalf("%s：课程学分只应计一次，out=%q", name, out)
			}
			// 失效申请不能重新参与满足要求：来源只能是有效免修 wok。
			if strings.Contains(out, "来源为通过修读") {
				t.Fatalf("%s：没有通过修读，不应出现修读来源，out=%q", name, out)
			}

			// 查看历史：三份申请各自的编号、依据、状态与原有原因对应原记录。
			out, _, code = runCLI(t, file, "show", "s1")
			if code != 0 {
				t.Fatalf("%s：合法混合历史应可查看，code=%d out=%q", name, code, out)
			}
			for _, want := range []string{
				`免修 wok：要求 r1，依据 "学科竞赛获奖"，状态：有效`,
				`免修 wrej：要求 r1，依据 ""，状态：已拒绝（免修依据为空）`,
				`免修 wrev：要求 r1，依据 "已过期的外校证明"，状态：已撤销（材料无法核实）`,
			} {
				if !strings.Contains(out, want) {
					t.Fatalf("%s：历史应包含原记录 %q，out=%q", name, want, out)
				}
			}
			if strings.Count(out, "免修 w") != 3 ||
				strings.Count(out, "状态：有效") != 1 ||
				strings.Count(out, "状态：已拒绝") != 1 ||
				strings.Count(out, "状态：已撤销") != 1 {
				t.Fatalf("%s：应恰好保留三份历史且各只有一个对应状态，out=%q", name, out)
			}

			// 合法记录的只读查询不得改动文件。
			if got, err := os.ReadFile(file); err != nil || string(got) != string(raw) {
				t.Fatalf("%s：只读查询后原文件应保持不变，err=%v", name, err)
			}
		})
	}
}

// TestCLILoadSameWaiverAndReqIDsAcrossStudents 唯一性只作用于同一学生名下：
// 不同学生各自使用相同的要求编号、甚至相同的免修编号，只要各自只有一份
// 有效申请，就应正常读取，不能误报为同一要求被重复取代。两人的要求对应
// 不同学分的课程，核对分别得到本人的课程学分与免修来源；各自的失效历史
// 不跨学生；合法记录的只读查询保持文件内容不变。
func TestCLILoadSameWaiverAndReqIDsAcrossStudents(t *testing.T) {
	w1s1 := diskWaiver("w1", "s1", "r1", "学科竞赛获奖", "approved", "")
	w2s1Rejected := diskWaiver("w2", "s1", "r1", "重复提交依据", "rejected", "该要求已有有效免修 w1")
	w1s2 := diskWaiver("w1", "s2", "r1", "外校修读证明", "approved", "")

	cases := map[string][]any{
		"s1的申请在前": {w1s1, w2s1Rejected, w1s2},
		"s2的申请在前": {w1s2, w2s1Rejected, w1s1},
	}
	for name, waivers := range cases {
		t.Run(name, func(t *testing.T) {
			record := map[string]any{
				"version": 1,
				"courses": []any{
					diskCourse("c1", "高等数学", 4),
					diskCourse("c2", "线性代数", 3),
				},
				"students": []any{
					map[string]any{"id": "s1"},
					map[string]any{"id": "s2"},
				},
				"requirements": []any{
					map[string]any{"id": "r1", "student": "s1", "course": "c1"},
					map[string]any{"id": "r1", "student": "s2", "course": "c2"},
				},
				"enrollments":   []any{},
				"waivers":       waivers,
				"nextResultSeq": 0,
			}
			file, raw := writePreexisting(t, record)

			// s1：4 学分，来源是本人 c1 上的 w1，另有本人的 w2 拒绝历史。
			out, errText, code := runCLI(t, file, "check", "s1")
			if code != 0 {
				t.Fatalf("%s：s1 核对应正常，code=%d out=%q err=%q",
					name, code, out, errText)
			}
			for _, want := range []string{
				"总学分：4",
				"要求 r1（课程 c1《高等数学》，4 学分）：已满足，来源为有效免修 w1",
				`免修 w2（要求 r1，依据 "重复提交依据"）`,
				"该要求已有有效免修 w1",
				"未满足要求：（无）",
			} {
				if !strings.Contains(out, want) {
					t.Fatalf("%s：s1 核对应包含 %q，out=%q", name, want, out)
				}
			}
			out, _, _ = runCLI(t, file, "show", "s1")
			if !strings.Contains(out, `免修 w1：要求 r1，依据 "学科竞赛获奖"，状态：有效`) ||
				!strings.Contains(out, `免修 w2：要求 r1，依据 "重复提交依据"，状态：已拒绝`) {
				t.Fatalf("%s：s1 历史应保留本人的 w1 有效与 w2 已拒绝，out=%q", name, out)
			}

			// s2：3 学分，来源是本人 c2 上的 w1，不能误报重复取代，
			// 也不能出现 s1 的拒绝历史。
			out, errText, code = runCLI(t, file, "check", "s2")
			if code != 0 {
				t.Fatalf("%s：s2 核对应正常而非被判重复取代，code=%d out=%q err=%q",
					name, code, out, errText)
			}
			for _, want := range []string{
				"总学分：3",
				"要求 r1（课程 c2《线性代数》，3 学分）：已满足，来源为有效免修 w1",
				"未满足要求：（无）",
			} {
				if !strings.Contains(out, want) {
					t.Fatalf("%s：s2 核对应包含 %q，out=%q", name, want, out)
				}
			}
			if strings.Contains(out, "被拒绝的免修") || strings.Contains(out, "w2") ||
				strings.Contains(out, "高等数学") {
				t.Fatalf("%s：s1 的课程与失效历史不应出现在 s2 名下，out=%q", name, out)
			}
			out, _, _ = runCLI(t, file, "show", "s2")
			if !strings.Contains(out, `免修 w1：要求 r1，依据 "外校修读证明"，状态：有效`) {
				t.Fatalf("%s：s2 历史应只有本人的 w1 与依据，out=%q", name, out)
			}
			if strings.Contains(out, "学科竞赛获奖") || strings.Contains(out, "w2") {
				t.Fatalf("%s：s2 历史不能混入 s1 的申请，out=%q", name, out)
			}

			// 合法记录的只读查询必须保持文件内容不变。
			if got, err := os.ReadFile(file); err != nil || string(got) != string(raw) {
				t.Fatalf("%s：只读查询后原文件应保持不变，err=%v", name, err)
			}
		})
	}
}
