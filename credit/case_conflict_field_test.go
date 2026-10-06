package credit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// escapedUpperCreditKey 解码后为 "Credit"（首字母大写，其余转义）。
const escapedUpperCreditKey = "\"\\u0043\\u0072edit\""

// TestLoadRejectsCaseInsensitiveFieldConflicts 同一 JSON 对象内两个字段名
// 只有大小写不同、却都会被现有读取功能识别为同一条记录字段时，整份文件
// 必须判为内容损坏：Load 报错（点名文件、规范字段名与冲突的两个原始
// 写法）、标记文件已存在，且原文件每个字节原样保留。字段顺序、是否相邻、
// 两个值是否相同都不影响结果；后一个值绝不能悄悄顶替前一个。
func TestLoadRejectsCaseInsensitiveFieldConflicts(t *testing.T) {
	cases := map[string]struct {
		content string
		field   string // 规范字段名
		first   string // 先出现的原始写法
		second  string // 后出现的原始写法
	}{
		"课程学分大小写冲突_小写在前": {
			`{"version":1,"courses":[` +
				`{"id":"c1","name":"数学","credit":4,"Credit":9,"open":true}]}` + "\n",
			"credit", "credit", "Credit",
		},
		"课程学分大小写冲突_大写在前": {
			// 颠倒顺序也必须拒绝，不能按后出现的 4 读取。
			`{"version":1,"courses":[` +
				`{"id":"c1","name":"数学","Credit":4,"credit":9,"open":true}]}` + "\n",
			"credit", "Credit", "credit",
		},
		"课程学分全大写与小写冲突": {
			`{"version":1,"courses":[` +
				`{"id":"c1","name":"数学","CREDIT":4,"credit":9,"open":true}]}` + "\n",
			"credit", "CREDIT", "credit",
		},
		"课程编号大小写冲突": {
			`{"version":1,"courses":[` +
				`{"id":"c1","ID":"c2","name":"数学","credit":4,"open":true}]}` + "\n",
			"id", "id", "ID",
		},
		"学生记录编号大小写冲突": {
			`{"version":1,"students":[{"id":"s1","ID":"s2"}]}` + "\n",
			"id", "id", "ID",
		},
		"要求记录学生字段大小写冲突": {
			`{"version":1,"students":[{"id":"s1"}],"courses":[` +
				`{"id":"c1","name":"数学","credit":4,"open":true}],"requirements":[` +
				`{"id":"r1","student":"s1","STUDENT":"s1","course":"c1"}]}` + "\n",
			"student", "student", "STUDENT",
		},
		"要求记录课程字段大小写冲突": {
			`{"version":1,"students":[{"id":"s1"}],"courses":[` +
				`{"id":"c1","name":"数学","credit":4,"open":true}],"requirements":[` +
				`{"id":"r1","student":"s1","course":"c1","Course":"c1"}]}` + "\n",
			"course", "course", "Course",
		},
		"修读记录学生字段大小写冲突": {
			`{"version":1,"students":[{"id":"s1"},{"id":"s2"}],"enrollments":[` +
				`{"id":"e1","student":"s1","Student":"s2","req":"r1",` +
				`"term":"2024春","result":"enrolled"}]}` + "\n",
			"student", "student", "Student",
		},
		"修读结果序号大小写冲突": {
			`{"version":1,"students":[{"id":"s1"}],"enrollments":[` +
				`{"id":"e1","student":"s1","req":"r1","term":"2024春",` +
				`"result":"passed","resultSeq":1,"ResultSeq":2}],"nextResultSeq":2}` + "\n",
			"resultSeq", "resultSeq", "ResultSeq",
		},
		"免修状态大小写冲突": {
			`{"version":1,"students":[{"id":"s1"}],"waivers":[` +
				`{"id":"w1","student":"s1","req":"r1","basis":"竞赛获奖",` +
				`"status":"rejected","STATUS":"approved"}]}` + "\n",
			"status", "status", "STATUS",
		},
		"顶层courses与大写形式冲突_后者空数组": {
			`{"version":1,"courses":[{"id":"c1","name":"数学","credit":4,"open":true}],` +
				`"COURSES":[]}` + "\n",
			"courses", "courses", "COURSES",
		},
		"顶层大写形式在前_小写后写空数组": {
			`{"version":1,"COURSES":[{"id":"c1","name":"数学","credit":4,"open":true}],` +
				`"courses":[],"students":[]}` + "\n",
			"courses", "COURSES", "courses",
		},
		"顶层students大小写冲突": {
			`{"version":1,"STUDENTS":[{"id":"s1"}],"students":[]}` + "\n",
			"students", "STUDENTS", "students",
		},
		"冲突字段不相邻且两个值相同": {
			`{"version":1,"courses":[` +
				`{"credit":4,"id":"c1","name":"数学","open":true,"Credit":4}]}` + "\n",
			"credit", "credit", "Credit",
		},
		"转义小写键与大写直接书写冲突": {
			`{"version":1,"courses":[` +
				`{"id":"c1","name":"数学",` + escapedCreditKey + `:4,"Credit":9,"open":true}]}` + "\n",
			"credit", "credit", "Credit",
		},
		"转义大写键与小写直接书写冲突": {
			`{"version":1,"courses":[` +
				`{"id":"c1","name":"数学","credit":4,` + escapedUpperCreditKey + `:9,"open":true}]}` + "\n",
			"credit", "credit", "Credit",
		},
		"大写顶层键数组内部记录冲突": {
			// 顶层用 COURSES 承载课程列表，数组元素仍须按课程对象检查。
			`{"version":1,"COURSES":[` +
				`{"id":"c1","name":"数学","credit":4,"Credit":9,"open":true}]}` + "\n",
			"credit", "credit", "Credit",
		},
		"冲突位于本次核对无关的课程": {
			`{"version":1,"courses":[` +
				`{"id":"c1","name":"数学","credit":4,"open":true},` +
				`{"id":"c9","name":"无关课程","credit":1,"CREDIT":2,"open":true}],` +
				`"students":[{"id":"s1"}],` +
				`"requirements":[{"id":"r1","student":"s1","course":"c1"}],` +
				`"enrollments":[{"id":"e1","student":"s1","req":"r1","term":"2024春",` +
				`"result":"passed","resultSeq":1}],"waivers":[],"nextResultSeq":1}` + "\n",
			"credit", "credit", "CREDIT",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "records.json")
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			s, existed, err := Load(path)
			if err == nil {
				t.Fatalf("大小写冲突文件必须拒绝读取，却得到 store=%v", s)
			}
			if !existed {
				t.Fatal("损坏文件应标记为已存在")
			}
			msg := err.Error()
			for _, want := range []string{path, "内容损坏", tc.field, tc.first, tc.second} {
				if !strings.Contains(msg, want) {
					t.Fatalf("错误信息应包含 %q，得到：%v", want, err)
				}
			}
			var dupErr *duplicateFieldError
			if errors.As(err, &dupErr) {
				t.Fatalf("Load 返回的错误不应泄露内部扫描错误类型，得到 %T", err)
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

// TestLoadCaseVariantAppearingOnceStillWorks 某个可识别字段在同一对象内只
// 出现一次时，现有读取功能能够识别的大小写写法必须继续有效；不同学生
// 编号值中的大小写按原文保留，s1 与 S1 仍是不同学生。
func TestLoadCaseVariantAppearingOnceStillWorks(t *testing.T) {
	// 全部字段只出现一次，但统统使用非规范大小写写法。
	content := `{
  "VERSION": 1,
  "COURSES": [
    {"ID": "c1", "NAME": "高等数学", "CREDIT": 4, "OPEN": true}
  ],
  "STUDENTS": [{"ID": "s1"}, {"ID": "S1"}],
  "REQUIREMENTS": [
    {"ID": "r1", "STUDENT": "s1", "COURSE": "c1"},
    {"ID": "r1", "Student": "S1", "Course": "c1"}
  ],
  "ENROLLMENTS": [
    {"ID": "e1", "STUDENT": "s1", "REQ": "r1", "TERM": "2024春",
     "RESULT": "passed", "RESULTSEQ": 1}
  ],
  "WAIVERS": [
    {"ID": "w1", "Student": "S1", "Req": "r1", "Basis": "竞赛获奖材料",
     "Status": "approved"}
  ],
  "NEXTRESULTSEQ": 1
}
`
	path := filepath.Join(t.TempDir(), "records.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	s, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("单次出现的大小写写法应正常读取：existed=%v err=%v", existed, err)
	}
	if c := s.Course("c1"); c == nil || c.Credit != 4 || !c.Open || c.Name != "高等数学" {
		t.Fatalf("课程应按大小写字段读入：%+v", c)
	}
	// s1 凭通过修读得 4 学分；S1 凭有效免修满足要求——大小写不同的编号值
	// 是两名不同学生，归属不得串行。
	if rep := s.CheckStudent("s1"); !rep.Found || rep.TotalCredits != 4 {
		t.Fatalf("s1 应按本人通过修读得 4 学分，得到 %+v", rep)
	}
	if rep := s.CheckStudent("S1"); !rep.Found || rep.TotalCredits != 4 {
		t.Fatalf("S1 应按本人有效免修得 4 学分，得到 %+v", rep)
	}
	if e := s.Enrollment("s1", "e1"); e == nil || e.StudentID != "s1" ||
		e.Result != Passed || e.ResultSeq != 1 {
		t.Fatalf("s1 的修读应按大小写字段读入且归属 s1：%+v", e)
	}
	if s.Enrollment("S1", "e1") != nil {
		t.Fatal("e1 只属于 s1，不能借大小写混到 S1 名下")
	}
	if w := s.Waiver("S1", "w1"); w == nil || w.Status != WaiverApproved ||
		w.ReqID != "r1" || w.Basis != "竞赛获奖材料" {
		t.Fatalf("S1 的有效免修应按大小写字段读入：%+v", w)
	}
}

// TestLoadUnknownFieldCaseVariantsUnchanged 不指向任何记录字段的未知键不
// 属于大小写归并限制：单个未知键继续按未知字段拒绝；两个大小写不同的
// 未知键也不会被误报成记录字段冲突，最终仍由结构体解码按未知字段拒绝，
// 行为与既有未知字段拒绝一致。
func TestLoadUnknownFieldCaseVariantsUnchanged(t *testing.T) {
	cases := map[string]string{
		"单个未知字段":    `{"version":1,"foo":1}`,
		"大小写两个未知字段": `{"version":1,"foo":1,"Foo":2}`,
		"课程对象内未知字段大小写变体": `{"version":1,"courses":[` +
			`{"id":"c1","name":"数学","credit":4,"open":true,"bar":1,"BAR":2}]}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "records.json")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			s, _, err := Load(path)
			if err == nil {
				t.Fatalf("含未知字段的文件仍应拒绝，却得到 store=%v", s)
			}
			var dupErr *duplicateFieldError
			if errors.As(err, &dupErr) {
				t.Fatalf("未知字段不应被判为记录字段冲突：%v", err)
			}
			if !strings.Contains(err.Error(), "JSON 解析失败") {
				t.Fatalf("应由未知字段规则（JSON 解析失败）拒绝，得到：%v", err)
			}
		})
	}
}

// TestScanDuplicateKeysCaseFolding 直接覆盖扫描器的大小写归并边界。
func TestScanDuplicateKeysCaseFolding(t *testing.T) {
	okCases := map[string]string{
		"课程只写一次大写Credit": `{"version":1,"courses":[` +
			`{"id":"c1","name":"数学","Credit":4,"open":true}]}`,
		"顶层只写一次大写COURSES": `{"VERSION":1,"COURSES":[]}`,
		"不同可识别字段不冲突": `{"version":1,"courses":[` +
			`{"id":"c1","name":"数学","credit":4,"open":true}]}`,
		"credit与course折叠后不同": `{"version":1,"courses":[` +
			`{"id":"c1","credit":4,"course":5}]}`,
		"不同课程各自的大小写写法": `{"version":1,"courses":[` +
			`{"id":"c1","Credit":4},{"id":"c2","CREDIT":9}]}`,
		"未知键大小写变体不归并": `{"foo":1,"Foo":2}`,
		"依据文字含大小写字段名不是键": `{"version":1,"waivers":[` +
			`{"id":"w1","basis":"credit Credit CREDIT"}]}`,
	}
	for name, in := range okCases {
		t.Run("合法/"+name, func(t *testing.T) {
			if err := scanDuplicateKeys(strings.NewReader(in)); err != nil {
				t.Fatalf("不应报冲突，得到 %v（输入 %q）", err, in)
			}
		})
	}

	dupCases := map[string]struct {
		in            string
		field         string
		first, second string
	}{
		"credit与Credit": {
			`{"version":1,"courses":[{"credit":4,"Credit":9}]}`,
			"credit", "credit", "Credit",
		},
		"Credit与credit顺序颠倒": {
			`{"version":1,"courses":[{"Credit":4,"credit":9}]}`,
			"credit", "Credit", "credit",
		},
		"顶层courses与COURSES": {
			`{"courses":[],"COURSES":[]}`, "courses", "courses", "COURSES",
		},
		"转义大写与直接小写": {
			`{"version":1,"courses":[{"credit":4,` + escapedUpperCreditKey + `:9}]}`,
			"credit", "credit", "Credit",
		},
		"转义小写与直接大写": {
			`{"version":1,"courses":[{` + escapedCreditKey + `:4,"Credit":9}]}`,
			"credit", "credit", "Credit",
		},
		"大写顶层数组内记录冲突": {
			`{"COURSES":[{"credit":4,"Credit":9}]}`,
			"credit", "credit", "Credit",
		},
		"嵌套数组深层记录冲突": {
			`{"ENROLLMENTS":[[{"student":"s1","Student":"s2"}]]}`,
			"student", "student", "Student",
		},
	}
	for name, tc := range dupCases {
		t.Run("冲突/"+name, func(t *testing.T) {
			err := scanDuplicateKeys(strings.NewReader(tc.in))
			var dupErr *duplicateFieldError
			if !errors.As(err, &dupErr) {
				t.Fatalf("应返回 duplicateFieldError（输入 %q），得到 %T: %v", tc.in, err, err)
			}
			if dupErr.field != tc.field || dupErr.first != tc.first || dupErr.second != tc.second {
				t.Fatalf("冲突信息不符：got (%q,%q,%q) want (%q,%q,%q)",
					dupErr.field, dupErr.first, dupErr.second,
					tc.field, tc.first, tc.second)
			}
		})
	}
}
