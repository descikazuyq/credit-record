package credit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件回归“课程开放状态被显式写成 JSON null 时，整份记录按内容损坏
// 拒绝”。
//
// 旧行为：课程的开放状态是非指针 bool，encoding/json 遇到 "open": null 不
// 报错，静默保留零值 false——没有确定状态的课程被读成明确停开：课程列表
// 显示停开、新增修读因此被拒，之后任何需要保存的操作还会把 false 落盘，
// 让未定状态变成明确的停开记录。
//
// 修复后：
//   - 课程对象显式给出指向 open 的字段（open、Open、OPEN 及解码后等同的
//     \uXXXX 转义写法，字段位置不限）而值为 null 时，Load 拒绝整份文件、
//     返回不可继续使用的 nil 记录集，错误点名记录文件与课程编号并说明开放
//     状态为空；问题课程尚未被要求引用、或本次查看另一名学生也同样拒绝，
//     不能跳过该课程继续办理；
//   - true（开放）与 false（停开）仍是合法状态：停开课程仍可建立要求，
//     停开前已有修读仍可提交成绩并参与核对，停开只限制新增修读；
//   - 省略 open 字段沿用既有读取规则，不按损坏处理；
//   - 课程名称、免修依据等普通文字“null”不是状态空值；最外层表示没有
//     记录的空数组或 null 列表继续按原规则读取，空记录仍能正常使用；
//   - 读取只读不修：拒绝后原文件全部字节保持不变，不补填状态、不删除
//     课程、不另存部分记录。
//
// null 值无法经正常登记命令产生，因此用例直接写出原始 JSON 记录文件。

// writeNullRawRecord 把原始 JSON 文本写入临时记录文件，返回路径与原始字节，
// 供拒绝读取后逐字节比对。
func writeNullRawRecord(t *testing.T, content string) (string, []byte) {
	t.Helper()
	raw := []byte(content)
	path := filepath.Join(t.TempDir(), "records.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("写入测试记录文件失败：%v", err)
	}
	return path, raw
}

// nullOpenBaseRecord 一份结构完整、引用齐全的记录：学生 s1 的要求 r1 指向
// 4 学分开放课程 c1 并有一条已通过修读 e1。另有第二门课程 c2 默认开放，
// 供用例把它的开放状态改坏。
func nullOpenBaseRecord() string {
	return `{
  "version": 1,
  "courses": [
    {"id": "c1", "name": "高等数学", "credit": 4, "open": true},
    {"id": "c2", "name": "线性代数", "credit": 3, "open": true}
  ],
  "students": [
    {"id": "s1"},
    {"id": "s2"}
  ],
  "requirements": [
    {"id": "r1", "student": "s1", "course": "c1"}
  ],
  "enrollments": [
    {"id": "e1", "student": "s1", "req": "r1", "term": "2024春", "result": "passed", "resultSeq": 1}
  ],
  "waivers": [],
  "nextResultSeq": 1
}
`
}

// assertNullOpenRejected 是 null 开放状态用例的共同断言：Load 必须返回
// nil 记录集与错误，文件标记为已存在，错误点名课程编号并说明开放状态为
// 空，且原文件字节完整保留。
func assertNullOpenRejected(t *testing.T, path string, raw []byte, courseID string) {
	t.Helper()
	s, existed, err := Load(path)
	if err == nil {
		t.Fatalf("显式 null 开放状态必须拒绝整份文件，却得到 store=%v", s)
	}
	if s != nil {
		t.Fatalf("拒绝时不得返回可继续使用的记录集，得到 %v", s)
	}
	if !existed {
		t.Fatalf("应按已有损坏文件处理（existed=true），existed=%v", existed)
	}
	msg := err.Error()
	for _, want := range []string{"内容损坏", courseID, "开放状态为空"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("错误应包含 %q，得到：%v", want, err)
		}
	}
	if got, readErr := os.ReadFile(path); readErr != nil || string(got) != string(raw) {
		t.Fatalf("拒绝读取不得改写原文件，readErr=%v", readErr)
	}
}

// TestLoadExplicitNullOpenRejectsWholeFile 主场景：c2 的开放状态为 null。
// 即使本次只核对 s1（其要求与修读全部只涉及 c1，c2 未被任何要求引用），
// 也不能跳过 c2 给出核对结果：整份文件判损坏，直接调用 Load 的程序收到
// 错误与 nil 记录集，原文件字节保持不变。
func TestLoadExplicitNullOpenRejectsWholeFile(t *testing.T) {
	content := strings.Replace(nullOpenBaseRecord(),
		`{"id": "c2", "name": "线性代数", "credit": 3, "open": true}`,
		`{"id": "c2", "name": "线性代数", "credit": 3, "open": null}`, 1)
	path, raw := writeNullRawRecord(t, content)

	assertNullOpenRejected(t, path, raw, "c2")

	// 错误信息应明确指出 null 不是开放状态，并给出合法取值含义。
	_, _, err := Load(path)
	if !strings.Contains(err.Error(), "null") ||
		!strings.Contains(err.Error(), "true") || !strings.Contains(err.Error(), "false") {
		t.Fatalf("错误应说明 open 被写成 null 且合法值为 true/false，得到：%v", err)
	}
}

// uescNull 拼出 JSON 文本中的一个 Unicode 转义序列：uescNull("006f")
// 返回六个字符（反斜线、u、0、0、6、f）。用函数拼接避免 Go 字符串直接
// 书写 \uXXXX 时被编译器转成对应字符。
func uescNull(hex string) string { return `\u` + hex }

// TestLoadNullOpenVariants 指向开放状态的各种字段写法（大小写与 Unicode
// 转义）对应 null 时都要拒绝，字段在课程对象中的位置不影响结果，错误都能
// 点名课程编号。
func TestLoadNullOpenVariants(t *testing.T) {
	cases := map[string]string{
		"小写 open":    `{"id": "c1", "name": "数学", "credit": 4, "open": null}`,
		"首字母大写 Open": `{"id": "c1", "name": "数学", "credit": 4, "Open": null}`,
		"全大写 OPEN":   `{"id": "c1", "name": "数学", "credit": 4, "OPEN": null}`,
		"混合大小写 OpeN": `{"id": "c1", "name": "数学", "credit": 4, "OpeN": null}`,
		"Unicode 转义 open": `{"id": "c1", "name": "数学", "credit": 4, "` +
			uescNull("006f") + uescNull("0070") + uescNull("0065") + uescNull("006e") + `": null}`,
		"Unicode 转义 Open": `{"id": "c1", "name": "数学", "credit": 4, "` +
			uescNull("004F") + uescNull("0070") + uescNull("0065") + uescNull("006e") + `": null}`,
		"转义与直写混合大小写": `{"id": "c1", "name": "数学", "credit": 4, "` +
			uescNull("004F") + uescNull("0070") + uescNull("0065") + `N": null}`,
		"open 写在最前": `{"open": null, "id": "c1", "name": "数学", "credit": 4}`,
		"open 夹在中间": `{"id": "c1", "open": null, "name": "数学", "credit": 4}`,
	}
	for name, course := range cases {
		t.Run(name, func(t *testing.T) {
			content := `{"version":1,"courses":[` + course + `],"students":[],"requirements":[],"enrollments":[],"waivers":[],"nextResultSeq":0}`
			path, raw := writeNullRawRecord(t, content)
			assertNullOpenRejected(t, path, raw, "c1")
		})
	}
}

// TestLoadNullOpenCourseWithoutID 课程同时缺少编号时仍按损坏拒绝，错误说明
// 该课程缺少编号，而不是因为“没有编号”这一另一种损坏放过 null 状态。
func TestLoadNullOpenCourseWithoutID(t *testing.T) {
	content := `{"version":1,"courses":[{"name":"数学","credit":4,"open":null}]}`
	path, raw := writeNullRawRecord(t, content)

	s, existed, err := Load(path)
	if err == nil || s != nil || !existed {
		t.Fatalf("缺编号且 open 为 null 的课程必须拒绝，s=%v existed=%v err=%v", s, existed, err)
	}
	if !strings.Contains(err.Error(), "开放状态为空") || !strings.Contains(err.Error(), "缺少编号") {
		t.Fatalf("错误应同时说明开放状态为空与缺少编号，得到：%v", err)
	}
	if got, readErr := os.ReadFile(path); readErr != nil || string(got) != string(raw) {
		t.Fatalf("拒绝读取不得改写原文件，readErr=%v", readErr)
	}
}

// TestLoadNullOpenReferencedCourseRejected 被要求与修读引用的课程开放状态
// 为 null 时同样整份拒绝——不能因为课程“在使用中”就把 null 将就成停开。
func TestLoadNullOpenReferencedCourseRejected(t *testing.T) {
	content := strings.Replace(nullOpenBaseRecord(),
		`{"id": "c1", "name": "高等数学", "credit": 4, "open": true}`,
		`{"id": "c1", "name": "高等数学", "credit": 4, "open": null}`, 1)
	path, raw := writeNullRawRecord(t, content)
	assertNullOpenRejected(t, path, raw, "c1")
}

// TestLoadFalseOpenIsLegalClosedCourse false 是合法的停开状态，不属损坏：
// 停开课程仍可建立要求，停开前已有修读仍可提交成绩并参与核对，只有新增
// 修读受限；重新保存后 false 原样保留。
func TestLoadFalseOpenIsLegalClosedCourse(t *testing.T) {
	// c1 停开；s1 名下有一条尚未提交结果的既有修读 e1。
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "高等数学", Credit: 4, Open: false},
		},
		Students:     []*Student{{ID: "s1"}},
		Requirements: []*Requirement{{ID: "r1", StudentID: "s1", CourseID: "c1"}},
		Enrollments: []*Enrollment{
			{ID: "e1", StudentID: "s1", ReqID: "r1", Term: "2024春", Result: Enrolled},
		},
	}
	path, _ := writeRecord(t, d)

	s, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("open:false 是合法停开状态，必须正常读取，existed=%v err=%v", existed, err)
	}
	c := s.Course("c1")
	if c == nil || c.Open {
		t.Fatalf("停开课程应按 false 读取，得到 %+v", c)
	}

	// 停开课程仍可建立要求：另一名学生对停开课程登记要求成功（s1 已就 c1
	// 有 r1，同一学生就同一课程的重复要求本就另按业务规则拒绝，与此无关）。
	if _, a, err := s.AddStudent("s2"); err != nil || a != ActionCreated {
		t.Fatalf("登记学生 s2 失败：action=%v err=%v", a, err)
	}
	if r, a, err := s.AddRequirement("s2", "r1", "c1"); err != nil || a != ActionCreated {
		t.Fatalf("停开课程应允许建立要求，action=%v err=%v r=%+v", a, err, r)
	}

	// 停开只限制新增修读。
	if _, _, err := s.AddEnrollment("s2", "r1", "2024春", "e9"); err == nil ||
		!strings.Contains(err.Error(), "已停开") {
		t.Fatalf("停开课程不能新增修读，得到 %v", err)
	}

	// 停开前已有的修读仍可提交成绩并参与核对。
	e, changed, err := s.SubmitResult("s1", "e1", Passed)
	if err != nil || !changed || e.Result != Passed {
		t.Fatalf("停开前已有修读应能提交通过，changed=%v err=%v e=%+v", changed, err, e)
	}
	rep := s.CheckStudent("s1")
	if rep.TotalCredits != 4 || len(rep.Unmet) != 0 {
		t.Fatalf("停开课程的已有通过修读仍计学分，得到 %+v", rep)
	}

	// 重新保存再加载，false 状态原样保留。
	path2 := filepath.Join(t.TempDir(), "records.json")
	if err := s.Save(path2); err != nil {
		t.Fatalf("Save: %v", err)
	}
	reloaded, _, err := Load(path2)
	if err != nil {
		t.Fatalf("含合法停开课程的记录重存后应可再次加载：%v", err)
	}
	if rc := reloaded.Course("c1"); rc == nil || rc.Open {
		t.Fatalf("重存后停开状态应保持 false，得到 %+v", rc)
	}
}

// TestLoadOmittedOpenKeepsLegacyRule 省略 open 字段不属本次纠正范围，沿用
// 既有读取规则（非指针 bool 的零值 false）：文件仍可正常打开并继续使用。
func TestLoadOmittedOpenKeepsLegacyRule(t *testing.T) {
	content := `{"version":1,"courses":[{"id":"c1","name":"数学","credit":4}],"students":[],"requirements":[],"enrollments":[],"waivers":[],"nextResultSeq":0}`
	path, raw := writeNullRawRecord(t, content)

	s, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("省略 open 字段沿用既有规则，不应判损坏，existed=%v err=%v", existed, err)
	}
	c := s.Course("c1")
	if c == nil {
		t.Fatal("课程应正常读取")
	}
	if c.Open {
		t.Fatalf("省略 open 时既有规则读为零值 false（停开），得到 open=%v", c.Open)
	}
	// 只读访问不改动文件。
	if got, readErr := os.ReadFile(path); readErr != nil || string(got) != string(raw) {
		t.Fatalf("只读访问不得改写原文件，readErr=%v", readErr)
	}
}

// TestLoadLiteralNullTextInStringsNotRejected 课程名称与免修依据中的普通
// 文字“null”只是字符串内容，不是开放状态空值：记录正常读取。
func TestLoadLiteralNullTextInStringsNotRejected(t *testing.T) {
	content := `{
  "version": 1,
  "courses": [
    {"id": "c1", "name": "null 概念课", "credit": 4, "open": true}
  ],
  "students": [{"id": "s1"}],
  "requirements": [{"id": "r1", "student": "s1", "course": "c1"}],
  "enrollments": [],
  "waivers": [
    {"id": "w1", "student": "s1", "req": "r9", "basis": "状态写成 null 也没有依据效力", "status": "rejected", "reason": "目标要求 r9 不存在或不属于该学生"}
  ],
  "nextResultSeq": 0
}
`
	path, _ := writeNullRawRecord(t, content)

	s, _, err := Load(path)
	if err != nil {
		t.Fatalf("名称/依据中的普通文字“null”不是状态空值，应正常读取：%v", err)
	}
	if c := s.Course("c1"); c == nil || c.Name != "null 概念课" || !c.Open {
		t.Fatalf("课程名称原文与开放状态应正常保留，得到 %+v", c)
	}
	rep := s.CheckStudent("s1")
	if len(rep.RejectedWaivers) != 1 {
		t.Fatalf("依据含“null”文字的被拒绝免修应保留在历史中，得到 %+v", rep.RejectedWaivers)
	}
}

// TestLoadNullAndEmptyListsStillRead 最外层五个列表写成 null 或空数组都
// 表示“没有记录”，继续按原规则读取，空记录可正常使用。
func TestLoadNullAndEmptyListsStillRead(t *testing.T) {
	cases := map[string]string{
		"列表全部为 null": `{"version":1,"courses":null,"students":null,"requirements":null,"enrollments":null,"waivers":null,"nextResultSeq":0}`,
		"列表全部为空数组":   `{"version":1,"courses":[],"students":[],"requirements":[],"enrollments":[],"waivers":[],"nextResultSeq":0}`,
		"只省略列表字段":    `{"version":1}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path, _ := writeNullRawRecord(t, content)
			s, existed, err := Load(path)
			if err != nil || !existed {
				t.Fatalf("空记录应正常读取，existed=%v err=%v", existed, err)
			}
			if len(s.Courses()) != 0 || len(s.Students()) != 0 {
				t.Fatalf("空记录不应包含任何课程或学生，得到 courses=%v students=%v",
					s.Courses(), s.Students())
			}
			// 空记录可直接继续办理。
			if _, a, err := s.AddCourse("c1", "数学", 4); err != nil || a != ActionCreated {
				t.Fatalf("空记录应可正常登记课程，action=%v err=%v", a, err)
			}
		})
	}
}
