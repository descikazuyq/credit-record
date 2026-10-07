package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件从命令行入口回归“读取免修历史时，状态为 rejected 的申请必须保留
// 含实际文字的拒绝原因”：
//   - 含缺原因已拒绝申请的文件，无论用哪条子命令访问（check、show、
//     list-courses 等只读命令，或 student/course/req/enroll/pass/waiver 等
//     登记命令），都在读取阶段以退出码 2 结束，标准输出不出现核对报告或
//     登记成功提示；
//   - 标准错误点名记录文件、学生编号与免修编号，并说明缺少拒绝原因；
//   - 缺失包括 reason 字段没出现、JSON null、空串、只含空白（空格、制表符、
//     换行、全角空格、不换行空格混用）；reason/Reason/REASON 及 Unicode
//     转义写法、字段位置都不影响结果；
//   - 即使申请不属于本次查询的学生，或目标要求已有通过修读、有效免修、照常
//     能算出学分，也不能跳过这份历史继续办理；
//   - 原文件逐字节保留：不补写推测的原因、不删除申请、不把状态改成有效。
//
// 同时固定不属本次纠正的行为：有实际文字的原因（含前后/中间空白）按原文
// 保留；有效免修原本可以没有 reason；已撤销免修沿用既有读取与核对行为。

// rejectedMissingReasonRecord 是一份结构完整、引用齐全的记录：学生 s1 的
// 要求 r1 指向 4 学分课程 c1，另有学生 s2 与课程 c2；只有 w1 是一条缺少
// 拒绝原因的已拒绝申请（原因写法由 reasonField 控制）。reasonField 为该
// 字段连同其值的片段（如 `"reason":null`），为空串表示完全不写该字段。
func rejectedMissingReasonRecord(reasonField string) string {
	reasonPart := ""
	if reasonField != "" {
		reasonPart = "," + reasonField
	}
	return "{\n" +
		`  "version": 1,` + "\n" +
		`  "courses": [` + "\n" +
		`    {"id": "c1", "name": "数学", "credit": 4, "open": true},` + "\n" +
		`    {"id": "c2", "name": "物理", "credit": 3, "open": true}` + "\n" +
		`  ],` + "\n" +
		`  "students": [{"id": "s1"}, {"id": "s2"}],` + "\n" +
		`  "requirements": [` + "\n" +
		`    {"id": "r1", "student": "s1", "course": "c1"},` + "\n" +
		`    {"id": "r2", "student": "s2", "course": "c2"}` + "\n" +
		`  ],` + "\n" +
		`  "enrollments": [` + "\n" +
		`    {"id": "e1", "student": "s1", "req": "r1", "term": "2024春", "result": "enrolled"}` + "\n" +
		`  ],` + "\n" +
		`  "waivers": [{` +
		`"id": "w1", "student": "s1", "req": "ghost", "basis": "旧材料",` +
		` "status": "rejected"` + reasonPart + `}], "nextResultSeq": 0` + "\n" +
		"}\n"
}

// TestCLIMissingRejectReasonRejectsEveryCommand 主场景：任何子命令访问含
// 缺原因已拒绝申请的文件都退出码 2、stdout 为空、stderr 点名文件/学生/免修
// 并说明缺少拒绝原因，原文件字节不变——包括查看另一名学生 s2、list-courses，
// 以及登记其他对象等写入类命令。
func TestCLIMissingRejectReasonRejectsEveryCommand(t *testing.T) {
	for name, field := range map[string]string{
		"字段缺失": "",
		"null": `"reason": null`,
		"空串":   `"reason": ""`,
	} {
		t.Run(name, func(t *testing.T) {
			content := rejectedMissingReasonRecord(field)
			commands := [][]string{
				{"check", "s1"},
				{"show", "s1"},
				// 本次只查看另一名学生：其记录完全合法，仍必须整份拒绝。
				{"check", "s2"},
				{"show", "s2"},
				{"list-courses"},
				{"student", "s9"},
				{"course", "c9", "化学", "2"},
				{"req", "s2", "r9", "c2"},
				{"enroll", "s1", "r1", "2024秋", "e9"},
				{"pass", "s1", "e1"},
				{"waiver", "s1", "r1", "w9", "新依据"},
				{"revoke-waiver", "s1", "w1", "撤销原因"},
			}
			for _, args := range commands {
				file := filepath.Join(t.TempDir(), "records.json")
				if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
				out, errText, code := runCLI(t, file, args...)
				if code != exitFile {
					t.Fatalf("原因缺失形式 %q：命令 %v 应退出码 %d，code=%d out=%q err=%q",
						name, args, exitFile, code, out, errText)
				}
				if out != "" {
					t.Fatalf("原因缺失形式 %q：命令 %v 不得输出核对报告/登记成功，out=%q",
						name, args, out)
				}
				for _, want := range []string{file, "内容损坏", "s1", "w1", "已拒绝免修", "拒绝原因"} {
					if !strings.Contains(errText, want) {
						t.Fatalf("原因缺失形式 %q：命令 %v 的错误应包含 %q，err=%q",
							name, args, want, errText)
					}
				}
				assertFileByteIdentical(t, file, []byte(content), "拒绝读取之后：")
			}
		})
	}
}

// TestCLIMissingRejectReasonBlankAndKeyVariants 各种空白混用，以及
// reason/Reason/REASON 与 Unicode 转义写法的字段、字段位置，都不影响拒绝。
func TestCLIMissingRejectReasonBlankAndKeyVariants(t *testing.T) {
	// 写入文件后 JSON 解码为空格、制表符、换行、全角空格、不换行空格、回车
	// 混用，TrimSpace 后为空；控制字符以 JSON 转义书写，直接放制表符进
	// JSON 字符串本身就是非法 JSON。
	blank := "\\t\\n 　\\u00a0 \\r "
	cases := map[string]string{
		"普通空白混用":        `"reason": "` + blank + `"`,
		"大写Reason为空白":   `"Reason": "` + blank + `"`,
		"全大写REASON为空串":  `"REASON": ""`,
		"转义小写键为null":    `"` + escapedReasonKeyLowerCLI + `": null`,
		"转义大写键为null":    `"` + escapedReasonKeyUpperCLI + `": null`,
		"部分转义混合键为空白":    `"reaso` + escapedUpperRCLI + `": "` + blank + `"`,
		"原因键在对象最前为null": `__FRONT__`,
	}
	for name, field := range cases {
		t.Run(name, func(t *testing.T) {
			var content string
			if field == "__FRONT__" {
				// 原因字段在最前，编号/学生都在其后：对象闭合时仍要点对名。
				content = "{\n" +
					`  "version": 1,` + "\n" +
					`  "courses": [{"id": "c1", "name": "数学", "credit": 4, "open": true}],` + "\n" +
					`  "students": [{"id": "s1"}],` + "\n" +
					`  "waivers": [{"reason": null, "id": "w7", "student": "s1",` +
					` "req": "ghost", "basis": "旧材料", "status": "rejected"}]` + "\n" +
					"}\n"
			} else {
				content = rejectedMissingReasonRecord(field)
			}
			file := filepath.Join(t.TempDir(), "records.json")
			if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			out, errText, code := runCLI(t, file, "check", "s1")
			if code != exitFile || out != "" {
				t.Fatalf("%s 应拒绝整份文件，code=%d out=%q err=%q", name, code, out, errText)
			}
			wantWaiver := "w1"
			if name == "原因键在对象最前为null" {
				wantWaiver = "w7"
			}
			if !strings.Contains(errText, wantWaiver) || !strings.Contains(errText, "s1") ||
				!strings.Contains(errText, "拒绝原因") {
				t.Fatalf("%s：错误应点名 s1 的免修 %s 缺少拒绝原因，err=%q",
					name, wantWaiver, errText)
			}
			assertFileByteIdentical(t, file, []byte(content), "拒绝之后：")
		})
	}
}

// 与 credit 包测试对应的 \u 转义键片段（main 包内另取名字，避免冲突）。
const (
	escapedReasonKeyLowerCLI = "\\u0072eason"
	escapedReasonKeyUpperCLI = "\\u0052EASON"
	escapedUpperRCLI         = "\\u0052"
)

// TestCLIMissingRejectReasonNotMaskedByCredits 即使问题申请的目标要求已由
// 通过修读满足、另有有效免修，照常能算出学分，check 也必须退出码 2、不输出
// 核对报告，不能跳过这份缺原因的历史继续办理。
func TestCLIMissingRejectReasonNotMaskedByCredits(t *testing.T) {
	t.Run("另有通过修读仍拒绝check", func(t *testing.T) {
		content := "{\n" +
			`  "version": 1,` + "\n" +
			`  "courses": [{"id": "c1", "name": "数学", "credit": 4, "open": true}],` + "\n" +
			`  "students": [{"id": "s1"}],` + "\n" +
			`  "requirements": [{"id": "r1", "student": "s1", "course": "c1"}],` + "\n" +
			`  "enrollments": [{"id": "e1", "student": "s1", "req": "r1",` +
			` "term": "2024春", "result": "passed", "resultSeq": 1}],` + "\n" +
			`  "waivers": [{"id": "wbad", "student": "s1", "req": "ghost",` +
			` "basis": "旧材料", "status": "rejected"}],` + "\n" +
			`  "nextResultSeq": 1` + "\n" +
			"}\n"
		file := filepath.Join(t.TempDir(), "records.json")
		if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		out, errText, code := runCLI(t, file, "check", "s1")
		if code != exitFile || out != "" {
			t.Fatalf("通过修读能算出学分也不能放过缺原因历史，code=%d out=%q err=%q",
				code, out, errText)
		}
		if !strings.Contains(errText, "wbad") || strings.Contains(errText, "总学分") {
			t.Fatalf("应只报 wbad 缺少拒绝原因，不能输出核对结果，err=%q", errText)
		}
		assertFileByteIdentical(t, file, []byte(content), "拒绝之后：")
	})

	t.Run("登记命令也不把缺原因历史继续保存", func(t *testing.T) {
		// 即使本次是会触发保存的登记命令，也必须先报文件错误，不能把这份
		// 不完整历史随新登记一起写回。
		content := rejectedMissingReasonRecord(`"reason": null`)
		file := filepath.Join(t.TempDir(), "records.json")
		if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		out, _, code := runCLI(t, file, "student", "s9")
		if code != exitFile || strings.Contains(out, "已登记学生") {
			t.Fatalf("登记命令遇缺原因历史应退出码 2 且无成功提示，code=%d out=%q", code, out)
		}
		assertFileByteIdentical(t, file, []byte(content), "登记被拒之后：")
	})
}

// TestCLIMissingRejectReasonBelongingToOtherStudent 缺原因的已拒绝申请属于
// s1，本次只查询完全合法的 s2：整份文件仍不可读，错误归属 s1，不返回 s2 的
// 部分核对结果。
func TestCLIMissingRejectReasonBelongingToOtherStudent(t *testing.T) {
	content := "{\n" +
		`  "version": 1,` + "\n" +
		`  "courses": [` + "\n" +
		`    {"id": "c1", "name": "数学", "credit": 4, "open": true},` + "\n" +
		`    {"id": "c2", "name": "物理", "credit": 3, "open": true}` + "\n" +
		`  ],` + "\n" +
		`  "students": [{"id": "s1"}, {"id": "s2"}],` + "\n" +
		`  "requirements": [` + "\n" +
		`    {"id": "r1", "student": "s1", "course": "c1"},` + "\n" +
		`    {"id": "r1", "student": "s2", "course": "c2"}` + "\n" +
		`  ],` + "\n" +
		`  "waivers": [` + "\n" +
		`    {"id": "wbad", "student": "s1", "req": "r1", "basis": "s1 的旧材料", "status": "rejected"},` + "\n" +
		`    {"id": "w1", "student": "s2", "req": "r1", "basis": "s2 的依据", "status": "approved"}` + "\n" +
		`  ]` + "\n" +
		"}\n"
	file := filepath.Join(t.TempDir(), "records.json")
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	out, errText, code := runCLI(t, file, "check", "s2")
	if code != exitFile || out != "" {
		t.Fatalf("属于 s1 的缺原因历史应让整份文件不可读，连 s2 都查不了，code=%d out=%q err=%q",
			code, out, errText)
	}
	if !strings.Contains(errText, "s1") || !strings.Contains(errText, "wbad") {
		t.Fatalf("错误应归属 s1 的免修 wbad，err=%q", errText)
	}
	if strings.Contains(errText, "学生 s2") {
		t.Fatalf("s2 的记录合法，不应被点名为问题方，err=%q", errText)
	}
	assertFileByteIdentical(t, file, []byte(content), "拒绝之后：")
}

// TestCLIRejectedWaiverWithRealReasonUnchanged 含实际文字的拒绝原因（含前后、
// 中间空白，不固定措辞）按原文保留；当初因目标要求不存在被拒绝、后来要求
// 已补建的申请仍是合法历史；因依据为空被拒绝的申请仍允许保留空依据。这些
// 历史继续出现在拒绝列表中、不提供学分，只读访问不改文件。
func TestCLIRejectedWaiverWithRealReasonUnchanged(t *testing.T) {
	reason := "\t 当时目标要求尚未建立 \n  "
	d := &diskRecord{
		Version:      1,
		Courses:      []diskCourse{{ID: "c1", Name: "数学", Credit: 4, Open: true}},
		Students:     []diskStudent{{ID: "s1"}},
		Requirements: []diskReq{{ID: "r1", Student: "s1", Course: "c1"}},
		Waivers: []diskWaiver{
			// 当初因目标要求 r1 不存在被拒绝；现在 r1 已补建，仍保留原原因。
			{ID: "w1", Student: "s1", Req: "r1", Basis: "竞赛材料",
				Status: "rejected", Reason: reason},
			// 因依据为空被拒绝：依据允许为空，原因是实际文字即可。
			{ID: "w2", Student: "s1", Req: "ghost", Basis: "",
				Status: "rejected", Reason: "免修依据为空"},
		},
	}
	file, raw := writeDiskRecord(t, d)

	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("含实际文字原因的拒绝历史应正常核对，code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "总学分：0") || !strings.Contains(out, "未满足要求：[r1]") {
		t.Fatalf("被拒绝历史不提供学分，out=%q", out)
	}
	if !strings.Contains(out, "免修 w1") || !strings.Contains(out, "当时目标要求尚未建立") ||
		!strings.Contains(out, "免修 w2") || !strings.Contains(out, "免修依据为空") {
		t.Fatalf("两条拒绝历史及原原因都应列出，out=%q", out)
	}

	// show 中原因按原文呈现（连同前后空白）。
	show, _, code := runCLI(t, file, "show", "s1")
	if code != 0 ||
		!strings.Contains(show, "状态：已拒绝（\t 当时目标要求尚未建立 \n  ）") ||
		!strings.Contains(show, `依据 ""，状态：已拒绝（免修依据为空）`) {
		t.Fatalf("show 应按原文保留原因（含空白）与空依据，show=%q", show)
	}
	assertFileByteIdentical(t, file, raw, "只读查询之后：")
}

// TestCLIApprovedAndRevokedWaiverWithoutReasonUnchanged 有效免修原本可以
// 没有 reason，已撤销免修沿用既有读取与核对行为：二者都不增加原因限制，
// 本次保持可正常查询与核对。
func TestCLIApprovedAndRevokedWaiverWithoutReasonUnchanged(t *testing.T) {
	d := &diskRecord{
		Version:      1,
		Courses:      []diskCourse{{ID: "c1", Name: "数学", Credit: 4, Open: true}},
		Students:     []diskStudent{{ID: "s1"}},
		Requirements: []diskReq{{ID: "r1", Student: "s1", Course: "c1"}},
		Waivers: []diskWaiver{
			// 有效免修没有 reason：合法，满足 r1。
			{ID: "wa", Student: "s1", Req: "r1", Basis: "竞赛获奖", Status: "approved"},
			// 已撤销免修没有 reason：沿用既有行为，不提供学分。
			{ID: "wv", Student: "s1", Req: "r1", Basis: "旧材料", Status: "revoked"},
		},
	}
	file, raw := writeDiskRecord(t, d)

	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "来源为有效免修 wa") ||
		!strings.Contains(out, "已撤销免修：[wv]") {
		t.Fatalf("有效免修应满足要求、已撤销免修照常列撤销历史，code=%d out=%q", code, out)
	}
	show, _, code := runCLI(t, file, "show", "s1")
	if code != 0 || !strings.Contains(show, "免修 wa") || !strings.Contains(show, "状态：有效") ||
		!strings.Contains(show, "免修 wv") || !strings.Contains(show, "状态：已撤销") {
		t.Fatalf("show 应正常展示无 reason 的有效/已撤销免修，show=%q", show)
	}
	assertFileByteIdentical(t, file, raw, "只读查询之后：")
}
