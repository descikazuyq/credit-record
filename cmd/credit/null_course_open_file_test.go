package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件从命令行入口回归“课程开放状态字段显式写成 JSON null 属于记录内容
// 损坏”：
//   - 这样的文件无论用哪条子命令访问，都在读取阶段以退出码 2 结束，标准
//     输出不出现课程列表、学生核对报告或登记成功提示；
//   - 标准错误点名记录文件与课程编号，并说明课程开放状态为空；
//   - 问题课程尚未被要求引用、或本次只查看另一名学生，也同样拒绝，不能
//     跳过这门课继续办理；
//   - open/Open/OPEN 及 Unicode 转义写法、字段在课程对象中的位置都不影响
//     结果；
//   - 原文件逐字节保留，即使用户发出的是“恢复开放”或其他写入类命令，也
//     不能借该操作把状态补写成 false/true。
//
// 同时固定不属本次纠正的行为：false 是合法停开状态（停开课程仍可建立要求、
// 停开前修读仍可提交成绩），省略 open 与空列表的既有读取规则不变，课程
// 名称与免修依据里的普通文字“null”不受影响。

// nullOpenRecord 是一份结构完整、引用齐全的记录：学生 s1 的要求 r1 指向
// 4 学分课程 c1，另有学生 s2 与开放课程 c2；只有 c1 的开放状态被显式写成
// null。nullKey 给出状态字段在文件中的写法（open/Open/OPEN/转义），keyAt
// >0 时把该字段放到课程对象最前。
func nullOpenRecord(nullKey string) string {
	return "{\n" +
		`  "version": 1,` + "\n" +
		`  "courses": [` + "\n" +
		`    {"id": "c1", "name": "数学", "credit": 4, "` + nullKey + `": null},` + "\n" +
		`    {"id": "c2", "name": "物理", "credit": 3, "open": true}` + "\n" +
		`  ],` + "\n" +
		`  "students": [{"id": "s1"}, {"id": "s2"}],` + "\n" +
		`  "requirements": [` + "\n" +
		`    {"id": "r1", "student": "s1", "course": "c1"},` + "\n" +
		`    {"id": "r2", "student": "s2", "course": "c2"}` + "\n" +
		`  ],` + "\n" +
		`  "enrollments": [` + "\n" +
		`    {"id": "e1", "student": "s1", "req": "r1", "term": "2024春", "result": "enrolled"}` + "\n" +
		`  ], "waivers": [], "nextResultSeq": 0` + "\n" +
		"}\n"
}

// TestCLINullCourseOpenRejectsEveryCommand 主场景：任何子命令访问含
// "open":null 课程的文件都退出码 2、stdout 为空、stderr 点名文件与课程、
// 原文件字节不变——包括查看另一名学生 s2、list-courses，以及恢复开放、
// 登记其他学生等写入类命令。
func TestCLINullCourseOpenRejectsEveryCommand(t *testing.T) {
	content := nullOpenRecord("open")
	commands := [][]string{
		{"list-courses"},
		{"check", "s1"},
		{"show", "s1"},
		// 本次只查看另一名学生：其要求指向完全合法的 c2，仍必须整份拒绝。
		{"check", "s2"},
		{"show", "s2"},
		{"student", "s9"},
		{"course", "c9", "化学", "2"},
		{"req", "s2", "r9", "c2"},
		{"enroll", "s1", "r1", "2024秋", "e9"},
		{"pass", "s1", "e1"},
		{"waiver", "s1", "r1", "w9", "新依据"},
		// 即使用户明确要恢复开放，也必须先报文件错误，不能借该操作补写状态。
		{"course-open", "c1"},
		{"course-close", "c2"},
	}
	for _, args := range commands {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "records.json")
			if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			out, errText, code := runCLI(t, file, args...)
			if code != exitFile {
				t.Fatalf("命令 %v 遇 open:null 应退出码 %d，code=%d out=%q err=%q",
					args, exitFile, code, out, errText)
			}
			if out != "" {
				t.Fatalf("命令 %v 不得输出课程列表/核对报告/登记成功，out=%q", args, out)
			}
			for _, want := range []string{file, "内容损坏", "c1", "开放状态为空"} {
				if !strings.Contains(errText, want) {
					t.Fatalf("命令 %v 的错误应包含 %q，err=%q", args, want, errText)
				}
			}
			assertFileByteIdentical(t, file, []byte(content), "拒绝读取之后：")
		})
	}
}

// TestCLINullCourseOpenKeyVariants open/Open/OPEN 以及解码后等同的 Unicode
// 转义写法都指向同一开放状态字段，写成 null 时一律拒绝。
func TestCLINullCourseOpenKeyVariants(t *testing.T) {
	keys := map[string]string{
		"小写open":  "open",
		"大写Open":  "Open",
		"全大写OPEN": "OPEN",
		"转义小写":    `\u006fpen`,
		"转义大写":    `\u004fPEN`,
		"部分转义":    `ope\u004e`,
	}
	for name, key := range keys {
		t.Run(name, func(t *testing.T) {
			content := nullOpenRecord(key)
			file := filepath.Join(t.TempDir(), "records.json")
			if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			out, errText, code := runCLI(t, file, "list-courses")
			if code != exitFile || out != "" {
				t.Fatalf("键写法 %q 的 null 状态应拒绝整份文件，code=%d out=%q err=%q",
					key, code, out, errText)
			}
			if !strings.Contains(errText, "c1") || !strings.Contains(errText, "开放状态为空") {
				t.Fatalf("错误应点名 c1 并说明开放状态为空，err=%q", errText)
			}
			assertFileByteIdentical(t, file, []byte(content), "拒绝之后：")
		})
	}
}

// TestCLINullOpenFieldPositionIrrelevant 字段在课程对象中的位置不影响结果，
// 编号出现在状态之后时错误仍要点名课程编号。
func TestCLINullOpenFieldPositionIrrelevant(t *testing.T) {
	content := "{\n" +
		`  "version": 1,` + "\n" +
		`  "courses": [` + "\n" +
		`    {"open": null, "credit": 4, "name": "数学", "id": "c1"}` + "\n" +
		`  ]` + "\n" +
		"}\n"
	file := filepath.Join(t.TempDir(), "records.json")
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	out, errText, code := runCLI(t, file, "list-courses")
	if code != exitFile || out != "" {
		t.Fatalf("open 字段在最前也应拒绝，code=%d out=%q err=%q", code, out, errText)
	}
	if !strings.Contains(errText, "c1") {
		t.Fatalf("编号在状态字段之后，错误仍要点名 c1，err=%q", errText)
	}
	assertFileByteIdentical(t, file, []byte(content), "拒绝之后：")
}

// TestCLINullOpenUnreferencedCourseRejected 问题课程尚未被任何要求引用：
// 文件里只有这门课与一名不相干学生，list-courses、check 该学生、登记新
// 对象都必须退出码 2，不能因为“用不到这门课”就跳过它继续办理。
func TestCLINullOpenUnreferencedCourseRejected(t *testing.T) {
	content := "{\n" +
		`  "version": 1,` + "\n" +
		`  "courses": [` + "\n" +
		`    {"id": "c9", "name": "无关课程", "credit": 1, "open": null}` + "\n" +
		`  ],` + "\n" +
		`  "students": [{"id": "s1"}]` + "\n" +
		"}\n"
	for _, args := range [][]string{
		{"list-courses"},
		{"check", "s1"},
		{"show", "s1"},
		{"student", "s2"},
		{"course-open", "c9"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "records.json")
			if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			out, errText, code := runCLI(t, file, args...)
			if code != exitFile || out != "" {
				t.Fatalf("未引用课程的 null 状态也必须拒绝命令 %v，code=%d out=%q err=%q",
					args, code, out, errText)
			}
			if !strings.Contains(errText, "c9") || !strings.Contains(errText, "开放状态为空") {
				t.Fatalf("错误应点名 c9 并说明开放状态为空，err=%q", errText)
			}
			assertFileByteIdentical(t, file, []byte(content), "拒绝之后：")
		})
	}
}

// TestCLIFalseOpenIsLegalClosedCourse false 是合法停开状态：课程列表显示
// 停开；停开课程仍可建立要求；停开只限制新增修读（退出码 1，不是文件
// 损坏）；停开前已有的修读仍可提交成绩并参与核对。
func TestCLIFalseOpenIsLegalClosedCourse(t *testing.T) {
	d := &diskRecord{
		Version: 1,
		Courses: []diskCourse{
			{ID: "c1", Name: "数学", Credit: 4, Open: false},
		},
		Students:     []diskStudent{{ID: "s1"}, {ID: "s2"}},
		Requirements: []diskReq{{ID: "r1", Student: "s1", Course: "c1"}},
		Enrollments: []diskEnr{
			{ID: "e1", Student: "s1", Req: "r1", Term: "2024春", Result: "enrolled"},
		},
	}
	file, _ := writeDiskRecord(t, d)

	out, _, code := runCLI(t, file, "list-courses")
	if code != 0 || !strings.Contains(out, "c1") || !strings.Contains(out, "状态：停开") {
		t.Fatalf("合法停开课程应在列表中显示停开，code=%d out=%q", code, out)
	}

	// 停开课程仍可建立要求。
	if out, _, code := runCLI(t, file, "req", "s2", "r2", "c1"); code != 0 {
		t.Fatalf("停开课程应仍可建立要求，code=%d out=%q", code, out)
	}
	// 新增修读受停开限制：业务拒绝退出码 1，而不是文件损坏 2。
	if _, errText, code := runCLI(t, file, "enroll", "s2", "r2", "2024秋", "e2"); code != exitRejected {
		t.Fatalf("停开课程新增修读应业务拒绝（退出码 %d），code=%d err=%q",
			exitRejected, code, errText)
	}
	// 停开前已有的修读仍可提交成绩。
	if out, _, code := runCLI(t, file, "pass", "s1", "e1"); code != 0 ||
		!strings.Contains(out, "结果已提交") {
		t.Fatalf("停开前的已有修读应能提交成绩，code=%d out=%q", code, out)
	}
	// 并参与核对。
	if out, _, code := runCLI(t, file, "check", "s1"); code != 0 ||
		!strings.Contains(out, "总学分：4") {
		t.Fatalf("停开课程的通过修读应照常计学分，code=%d out=%q", code, out)
	}
	// 上述写入（建要求、提交成绩）合法落盘后，文件仍能正常打开，且 c1 的
	// 合法停开状态不被任何操作改写。
	if out, _, code := runCLI(t, file, "list-courses"); code != 0 ||
		!strings.Contains(out, "课程 c1《数学》4 学分，状态：停开") {
		t.Fatalf("办理后 c1 应仍为合法停开且文件可正常打开，code=%d out=%q", code, out)
	}
}

// TestCLINullTextInCourseNameAndBasisUnchanged 课程名称、免修依据中的普通
// 文字“null”是字符串值，不是开放状态空值，文件正常读取。
func TestCLINullTextInCourseNameAndBasisUnchanged(t *testing.T) {
	d := &diskRecord{
		Version: 1,
		Courses: []diskCourse{
			{ID: "c1", Name: "null", Credit: 4, Open: true},
		},
		Students: []diskStudent{{ID: "s1"}},
		Waivers: []diskWaiver{
			{ID: "w1", Student: "s1", Req: "rX", Basis: "依据里提到 null",
				Status: "rejected", Reason: "目标要求 rX 不存在或不属于该学生"},
		},
	}
	file, raw := writeDiskRecord(t, d)

	out, _, code := runCLI(t, file, "list-courses")
	if code != 0 || !strings.Contains(out, "《null》") {
		t.Fatalf("课程名称为文字 null 应正常显示，code=%d out=%q", code, out)
	}
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "被拒绝的免修") {
		t.Fatalf("依据含文字 null 的被拒绝免修应正常列出，code=%d out=%q", code, out)
	}
	assertFileByteIdentical(t, file, raw, "只读访问之后：")
}

// TestCLIOmittedOpenAndEmptyListsKeepLegacyRead 省略 open 字段的既有读取
// 规则与空数组/null 列表按空记录读取的规则都不在本次纠正范围内，保持不变。
func TestCLIOmittedOpenAndEmptyListsKeepLegacyRead(t *testing.T) {
	// 省略 open：既有读取规则按零值（停开）处理，文件能打开、列表能显示。
	omitted := `{"version":1,"courses":[{"id":"c1","name":"数学","credit":4}]}` + "\n"
	file1 := filepath.Join(t.TempDir(), "records.json")
	if err := os.WriteFile(file1, []byte(omitted), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _, code := runCLI(t, file1, "list-courses")
	if code != 0 || !strings.Contains(out, "状态：停开") {
		t.Fatalf("省略 open 应按既有规则读成停开并正常显示，code=%d out=%q", code, out)
	}

	// 空数组与 null 列表：空记录正常使用。
	for _, content := range []string{
		`{"version":1,"courses":null,"students":[],"requirements":null,` +
			`"enrollments":[],"waivers":null}` + "\n",
		`{"version":1}` + "\n",
	} {
		file2 := filepath.Join(t.TempDir(), "records.json")
		if err := os.WriteFile(file2, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if out, _, code := runCLI(t, file2, "list-courses"); code != 0 ||
			!strings.Contains(out, "（尚无课程）") {
			t.Fatalf("空数组/null 列表应按空记录读取，content=%q code=%d out=%q",
				content, code, out)
		}
	}
}
