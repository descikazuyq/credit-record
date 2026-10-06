package credit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件回归“课程开放状态字段显式写成 JSON null 属于记录内容损坏”。
//
// 旧实现把课程开放状态存成普通 bool：标准库把 "open":null 解进 bool 时既不
// 报错也不改字段，零值 false 被原样保留——于是“状态尚未确定”的记录被读成
// 明确“停开”：课程列表显示停开、新增修读被误拒，之后任何一次需要保存的
// 操作还会把 null 固化成 false。修复后这样的文件整份判为损坏：
//   - Load 直接返回错误，不返回可继续使用的记录集，文件标记为已存在；
//   - 错误点名记录文件与课程编号，并说明课程开放状态为空；
//   - open/Open/OPEN 及解码后等同的 Unicode 转义写法都指向同一字段，字段
//     在课程对象中的位置不影响结果，编号出现在状态之后也照样点名；
//   - 问题课程尚未被要求引用、记录里另有与它无关的学生，也不跳过它继续
//     办理；
//   - 只读检查，原文件逐字节保留。
//
// 不在本次纠正范围：字段完全省略时沿用既有读取规则；true/false 是合法
// 状态，false（停开）课程仍可建立要求、已有修读仍可提交成绩并参与核对；
// 课程名称、免修依据中的普通文字“null”不是状态空值；表示空列表的
// "courses":null 等照常按没有记录读取。

// JSON 源文本中的 \uXXXX 转义键片段（不含两侧引号，引号由用例拼接），
// 写入文件后 JSON 解码分别为 "open"、"OPEN" 与大写 "N"。这里用 Go 原始
// 字符串，反斜线逐字保留，正好模拟记录文件中的 Unicode 转义写法。
const (
	escapedOpenKeyLower = `\u006fpen`
	escapedOpenKeyUpper = `\u004fPEN`
	escapedUpperN       = `\u004e`
)

// nullOpenRejectCases 各案的课程编号、名称与学分都合法，只有开放状态被
// 显式写成 null。wantKey 是错误信息应点名的字段原始写法（解码后文字）。
var nullOpenRejectCases = map[string]struct {
	content string
	course  string
	wantKey string
}{
	"小写open显式null": {
		`{"version":1,"courses":[` +
			`{"id":"c1","name":"数学","credit":4,"open":null}]}` + "\n",
		"c1", "open",
	},
	"首字母大写Open": {
		`{"version":1,"courses":[` +
			`{"id":"c1","name":"数学","credit":4,"Open":null}]}` + "\n",
		"c1", "Open",
	},
	"全大写OPEN": {
		`{"version":1,"courses":[` +
			`{"id":"c1","name":"数学","credit":4,"OPEN":null}]}` + "\n",
		"c1", "OPEN",
	},
	"状态字段在最前位置不影响": {
		`{"version":1,"courses":[` +
			`{"open":null,"id":"c1","name":"数学","credit":4}]}` + "\n",
		"c1", "open",
	},
	"编号出现在null之后仍点名课程": {
		`{"version":1,"courses":[` +
			`{"name":"数学","credit":4,"open":null,"id":"c1"}]}` + "\n",
		"c1", "open",
	},
	"unicode转义小写键": {
		`{"version":1,"courses":[` +
			`{"id":"c1","name":"数学","credit":4,"` + escapedOpenKeyLower + `":null}]}` + "\n",
		"c1", "open",
	},
	"unicode转义大写键": {
		`{"version":1,"courses":[` +
			`{"id":"c1","name":"数学","credit":4,"` + escapedOpenKeyUpper + `":null}]}` + "\n",
		"c1", "OPEN",
	},
	"部分转义混合大小写": {
		`{"version":1,"courses":[` +
			`{"id":"c1","name":"数学","credit":4,"ope` + escapedUpperN + `":null}]}` + "\n",
		"c1", "opeN",
	},
	"顶层COURSES变体里的null状态": {
		`{"version":1,"COURSES":[` +
			`{"id":"c1","name":"数学","credit":4,"open":null}]}` + "\n",
		"c1", "open",
	},
	"问题课程未被任何要求引用": {
		`{"version":1,"courses":[` +
			`{"id":"c9","name":"无关课程","credit":1,"open":null}],` +
			`"students":[{"id":"s1"}]}` + "\n",
		"c9", "open",
	},
	"多门课程中第二门状态为空": {
		`{"version":1,"courses":[` +
			`{"id":"c1","name":"数学","credit":4,"open":true},` +
			`{"id":"c2","name":"物理","credit":3,"Open":null}]}` + "\n",
		"c2", "Open",
	},
	"嵌套数组中的课程状态为空": {
		`{"version":1,"courses":[[` +
			`{"id":"c3","name":"化学","credit":2,"open":null}]]}` + "\n",
		"c3", "open",
	},
}

func TestLoadRejectsNullCourseOpen(t *testing.T) {
	for name, tc := range nullOpenRejectCases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "records.json")
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			s, existed, err := Load(path)
			if err == nil {
				t.Fatalf("开放状态显式为 null 必须拒绝整份文件，却得到 store=%v", s)
			}
			if s != nil {
				t.Fatalf("拒绝时不得返回可继续使用的记录集，得到 %v", s)
			}
			if !existed {
				t.Fatal("损坏文件应标记为已存在")
			}
			msg := err.Error()
			for _, want := range []string{path, "内容损坏", tc.course, "开放状态为空", "null", tc.wantKey} {
				if !strings.Contains(msg, want) {
					t.Fatalf("错误信息应包含 %q，得到：%v", want, err)
				}
			}
			got, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(got) != tc.content {
				t.Fatalf("拒绝读取不得改动原文件\nwant=%q\n got=%q", tc.content, got)
			}
		})
	}
}

// TestLoadNullOpenCourseRejectedEvenWhenUnrelated 记录结构完整：s1 的要求
// 指向另一门开放课程，null 开放状态的 c2 没有任何人引用；即使本次只想核对
// s1，Load 也必须整体失败——不能跳过 c2 办理。
func TestLoadNullOpenCourseRejectedEvenWhenUnrelated(t *testing.T) {
	content := `{
  "version": 1,
  "courses": [
    {"id": "c1", "name": "数学", "credit": 4, "open": true},
    {"id": "c2", "name": "物理", "credit": 3, "open": null}
  ],
  "students": [{"id": "s1"}],
  "requirements": [{"id": "r1", "student": "s1", "course": "c1"}],
  "enrollments": [
    {"id": "e1", "student": "s1", "req": "r1", "term": "2024春",
     "result": "passed", "resultSeq": 1}
  ],
  "waivers": [],
  "nextResultSeq": 1
}
`
	path := filepath.Join(t.TempDir(), "records.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	s, existed, err := Load(path)
	if err == nil {
		t.Fatalf("未引用的 null 状态课程也必须整份拒绝，却得到 store=%v", s)
	}
	if s != nil || !existed {
		t.Fatalf("应返回 nil 记录集并标记文件已存在，s=%v existed=%v", s, existed)
	}
	if msg := err.Error(); !strings.Contains(msg, "c2") || !strings.Contains(msg, "开放状态为空") {
		t.Fatalf("错误应点名课程 c2 并说明开放状态为空，得到：%v", err)
	}
	if got, readErr := os.ReadFile(path); readErr != nil || string(got) != content {
		t.Fatalf("拒绝读取不得改写原文件，readErr=%v", readErr)
	}
}

// TestScanNullCourseOpen 直接覆盖扫描器边界：只有课程对象内指向开放状态
// 字段、且值 token 确为 JSON null 时才报错。
func TestScanNullCourseOpen(t *testing.T) {
	badCases := map[string]string{
		"小写":           `{"version":1,"courses":[{"id":"c1","open":null}]}`,
		"字段在最前":        `{"courses":[{"open":null,"id":"c1"}]}`,
		"大写变体":         `{"courses":[{"id":"c1","OPEN":null}]}`,
		"转义键":          `{"courses":[{"id":"c1","` + escapedOpenKeyLower + `":null}]}`,
		"多层嵌套数组":       `{"courses":[[{"id":"c1","open":null}]]}`,
		"课程在多元素数组末尾":   `{"courses":[{"id":"c1","open":true},{"id":"c2","open":false},{"id":"c3","open":null}]}`,
		"先null后id仍能定位": `{"courses":[{"open":null,"credit":2,"name":"x","id":"c7"}]}`,
	}
	for name, in := range badCases {
		t.Run("拒绝/"+name, func(t *testing.T) {
			var e *nullCourseOpenError
			err := scanNullCourseOpen(strings.NewReader(in))
			if !errors.As(err, &e) {
				t.Fatalf("应返回 *nullCourseOpenError（输入 %q），得到 %T: %v", in, err, err)
			}
		})
	}

	okCases := map[string]string{
		"open为true":        `{"version":1,"courses":[{"id":"c1","credit":4,"open":true}]}`,
		"open为false合法停开":   `{"version":1,"courses":[{"id":"c1","credit":4,"open":false}]}`,
		"省略open":           `{"version":1,"courses":[{"id":"c1","credit":4}]}`,
		"课程名称是文字null":      `{"courses":[{"id":"c1","name":"null","credit":4,"open":true}]}`,
		"课程名称含null字样":      `{"courses":[{"id":"c1","name":"null 导论","credit":4}]}`,
		"免修依据是文字null":      `{"students":[{"id":"s1"}],"waivers":[{"id":"w1","student":"s1","req":"r","basis":"null","status":"rejected"}]}`,
		"courses是空数组":      `{"version":1,"courses":[]}`,
		"courses是null空列表":  `{"version":1,"courses":null}`,
		"各列表均为null":        `{"version":1,"courses":null,"students":null,"requirements":null,"enrollments":null,"waivers":null}`,
		"最外层null按原规则":      `null`,
		"最外层空数组按原规则":       `[]`,
		"课程内未知字段为null":     `{"courses":[{"id":"c1","credit":4,"open":true,"bogus":null}]}`,
		"嵌套对象内的open不属课程状态": `{"courses":[{"id":"c1","credit":4,"open":true,"extra":{"open":null}}]}`,
		"open的值是字符串null":   `{"courses":[{"id":"c1","credit":4,"open":"null"}]}`,
		"open的值是数字":        `{"courses":[{"id":"c1","credit":4,"open":0}]}`,
		"空输入留给解码阶段":        ``,
		"语法不完整留给解码阶段":      `{"courses":[{"id":"c1","open":`,
	}
	for name, in := range okCases {
		t.Run("放过/"+name, func(t *testing.T) {
			if err := scanNullCourseOpen(strings.NewReader(in)); err != nil {
				t.Fatalf("不应报告开放状态为空（输入 %q），得到 %v", in, err)
			}
		})
	}
}

// TestLoadOmittedOpenKeepsLegacyReadRule 本次只纠正“显式 null”：字段完全
// 省略的课程继续按既有读取规则（零值 false，即停开）读取，不扩大判定。
func TestLoadOmittedOpenKeepsLegacyReadRule(t *testing.T) {
	content := `{"version":1,"courses":[` +
		`{"id":"c1","name":"数学","credit":4}]}` + "\n"
	path := filepath.Join(t.TempDir(), "records.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	s, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("省略 open 字段应按既有规则读取，existed=%v err=%v", existed, err)
	}
	c := s.Course("c1")
	if c == nil || c.Open {
		t.Fatalf("省略 open 的既有读取结果应是停开（false），得到 %+v", c)
	}
}

// TestLoadClosedCourseStillUsable false 是合法停开状态：停开课程仍可建立
// 要求，停开前已有的修读仍能提交成绩并参与核对，停开只限制新增修读。
func TestLoadClosedCourseStillUsable(t *testing.T) {
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "数学", Credit: 4, Open: false},
			{ID: "c2", Name: "物理", Credit: 3, Open: true},
		},
		Students: []*Student{{ID: "s1"}, {ID: "s2"}},
		Requirements: []*Requirement{
			{ID: "r1", StudentID: "s1", CourseID: "c1"},
		},
		Enrollments: []*Enrollment{
			{ID: "e1", StudentID: "s1", ReqID: "r1", Term: "2024春", Result: Enrolled},
		},
	}
	path, raw := writeRecord(t, d)

	s, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("显式 false 的停开课程必须正常读取，existed=%v err=%v", existed, err)
	}
	if c := s.Course("c1"); c == nil || c.Open {
		t.Fatalf("c1 应为合法停开课程，得到 %+v", c)
	}

	// 停开课程仍可建立要求。
	if r, a, err := s.AddRequirement("s2", "r2", "c1"); err != nil || a != ActionCreated {
		t.Fatalf("停开课程应仍可建立要求，得到 %+v action=%v err=%v", r, a, err)
	}
	// 停开只限制新增修读。
	if _, _, err := s.AddEnrollment("s2", "r2", "2024秋", "e2"); err == nil ||
		!strings.Contains(err.Error(), "已停开") {
		t.Fatalf("停开课程新增修读应被拒绝，得到 %v", err)
	}
	// 停开前已有的修读仍能提交成绩。
	if e, changed, err := s.SubmitResult("s1", "e1", Passed); err != nil || !changed ||
		e.Result != Passed {
		t.Fatalf("停开前的已有修读应能提交通过，得到 %+v changed=%v err=%v", e, changed, err)
	}
	// 并参与核对：s1 得到该课程 4 学分。
	if rep := s.CheckStudent("s1"); !rep.Found || rep.TotalCredits != 4 ||
		len(rep.Unmet) != 0 {
		t.Fatalf("停开课程的通过修读应照常计学分，得到 %+v", rep)
	}
	// 重新保存后仍可读取且状态仍为停开。
	path2 := filepath.Join(t.TempDir(), "r2.json")
	if err := s.Save(path2); err != nil {
		t.Fatalf("Save: %v", err)
	}
	reloaded, _, err := Load(path2)
	if err != nil {
		t.Fatalf("合法停开记录保存后应可再次读取：%v", err)
	}
	if c := reloaded.Course("c1"); c == nil || c.Open {
		t.Fatalf("重存后 c1 应仍为停开，得到 %+v", c)
	}
	if got, readErr := os.ReadFile(path); readErr != nil || string(got) != string(raw) {
		t.Fatalf("原文件不应被只读操作改写，readErr=%v", readErr)
	}
}

// TestLoadNullTextInStringFieldsUnchanged 课程名称、免修依据里的普通文字
// “null”是字符串值，不是开放状态空值，记录照常读取。
func TestLoadNullTextInStringFieldsUnchanged(t *testing.T) {
	d := &fileData{
		Version: recordVersion,
		Courses: []*Course{
			{ID: "c1", Name: "null", Credit: 4, Open: true},
		},
		Students: []*Student{{ID: "s1"}},
		Waivers: []*Waiver{
			{ID: "w1", StudentID: "s1", ReqID: "rX", Basis: "课程名称里也有 null",
				Status: WaiverRejected, Reason: "目标要求 rX 不存在或不属于该学生"},
		},
	}
	path, _ := writeRecord(t, d)
	s, _, err := Load(path)
	if err != nil {
		t.Fatalf("字符串值中的普通文字 null 不应触发损坏：%v", err)
	}
	if c := s.Course("c1"); c == nil || c.Name != "null" || !c.Open {
		t.Fatalf("课程名称为文字 null 的课程应原样读取，得到 %+v", c)
	}
}

// TestLoadNullAndEmptyListsReadAsNoRecords 最外层各记录列表写成空数组或
// null 都表示没有该类记录，继续按原规则读取为空记录。
func TestLoadNullAndEmptyListsReadAsNoRecords(t *testing.T) {
	content := `{"version":1,"courses":null,"students":[],"requirements":null,` +
		`"enrollments":[],"waivers":null,"nextResultSeq":0}` + "\n"
	path := filepath.Join(t.TempDir(), "records.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	s, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("空数组/null 列表应按空记录读取，existed=%v err=%v", existed, err)
	}
	if len(s.Courses()) != 0 || len(s.Students()) != 0 {
		t.Fatalf("应为空记录，得到课程=%d 学生=%d", len(s.Courses()), len(s.Students()))
	}
}
