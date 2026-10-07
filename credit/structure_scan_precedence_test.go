package credit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件回归字段重复检查与课程开放状态为空检查共用同一套记录结构识别口径
// 后的优先级：同一份文件里既有字段冲突、又有课程把开放状态显式写成 null
// 时，无论问题课程排在冲突字段之前还是之后，都必须按字段冲突拒绝（不因
// 课程排列位置改变错误类别）。

// dupAndNullOpenContent 构造同时含两类问题的记录。
// nullFirst=true 时把 open:null 的课程放在数组最前、字段冲突课程在后；
// 反之字段冲突课程在前。字段冲突为课程对象内 "credit" 与 "Credit"。
func dupAndNullOpenContent(nullFirst bool) string {
	nullCourse := `{"id":"c1","name":"数学","credit":4,"open":null}`
	dupCourse := `{"id":"c2","name":"物理","credit":3,"Credit":8,"open":true}`
	courses := nullCourse + "," + dupCourse
	if !nullFirst {
		courses = dupCourse + "," + nullCourse
	}
	return `{"version":1,"courses":[` + courses + `],"students":[{"id":"s1"}]}` + "\n"
}

func TestLoadFieldConflictPrecedenceOverNullOpen(t *testing.T) {
	for _, nullFirst := range []bool{true, false} {
		name := "空状态课程在前"
		if !nullFirst {
			name = "冲突字段课程在前"
		}
		t.Run(name, func(t *testing.T) {
			content := dupAndNullOpenContent(nullFirst)
			path := filepath.Join(t.TempDir(), "records.json")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			s, existed, err := Load(path)
			if err == nil {
				t.Fatalf("字段冲突与空状态并存必须拒绝读取，却得到 store=%v", s)
			}
			if s != nil || !existed {
				t.Fatalf("应返回 nil 记录集并标记文件已存在，s=%v existed=%v", s, existed)
			}
			msg := err.Error()
			for _, want := range []string{path, "内容损坏", "字段归属无法确定", "credit", "Credit"} {
				if !strings.Contains(msg, want) {
					t.Fatalf("应优先报告字段冲突（含 %q），得到：%v", want, err)
				}
			}
			if strings.Contains(msg, "开放状态为空") {
				t.Fatalf("字段冲突必须优先于空状态报告，得到：%v", err)
			}
			if got, readErr := os.ReadFile(path); readErr != nil || string(got) != content {
				t.Fatalf("拒绝读取不得改动原文件，readErr=%v", readErr)
			}
		})
	}
}

// TestScanRecordStructurePrecedence 直接覆盖合并扫描器：两类问题并存时一律
// 返回 *duplicateFieldError；只有空状态时才返回 *nullCourseOpenError。
func TestScanRecordStructurePrecedence(t *testing.T) {
	for _, nullFirst := range []bool{true, false} {
		err := scanRecordStructure(
			strings.NewReader(dupAndNullOpenContent(nullFirst)),
			structureChecks{duplicateKeys: true, nullOpen: true})
		var dupErr *duplicateFieldError
		if !errors.As(err, &dupErr) {
			t.Fatalf("nullFirst=%v 时应报字段冲突，得到 %T: %v", nullFirst, err, err)
		}
		var openErr *nullCourseOpenError
		if errors.As(err, &openErr) {
			t.Fatalf("nullFirst=%v 时字段冲突应优先，却报了空状态：%v", nullFirst, err)
		}
	}

	// 只有空状态、没有冲突时，双重检查返回空状态错误。
	err := scanRecordStructure(
		strings.NewReader(`{"version":1,"courses":[{"id":"c1","credit":4,"open":null}]}`),
		structureChecks{duplicateKeys: true, nullOpen: true})
	var openErr *nullCourseOpenError
	if !errors.As(err, &openErr) || openErr.courseID != "c1" || openErr.key != "open" {
		t.Fatalf("仅有空状态时应返回 *nullCourseOpenError，得到 %T: %v", err, err)
	}

	// 合法记录在双重检查下不报错（特别注意不能返回带 nil 指针的非空 error）。
	ok := `{"version":1,"courses":[{"id":"c1","name":"数学","credit":4,"open":true}],` +
		`"students":[],"waivers":[]}`
	if err := scanRecordStructure(strings.NewReader(ok),
		structureChecks{duplicateKeys: true, nullOpen: true}); err != nil {
		t.Fatalf("合法记录不应报错，得到 %T: %v", err, err)
	}
}

// TestScanRecordStructureNullOpenDeferredCourseID 空状态课程排在最前、编号
// 在 open:null 之后，且同一文件后段另有字段冲突：字段冲突优先返回；若去掉
// 冲突（只留空状态），延后到课程对象读完时仍能凭后出现的编号点名课程。
func TestScanRecordStructureNullOpenDeferredCourseID(t *testing.T) {
	in := `{"courses":[{"open":null,"credit":2,"name":"x","id":"c7"}]}`
	err := scanRecordStructure(strings.NewReader(in),
		structureChecks{duplicateKeys: true, nullOpen: true})
	var openErr *nullCourseOpenError
	if !errors.As(err, &openErr) || openErr.courseID != "c7" {
		t.Fatalf("编号出现在 null 之后仍应点名 c7，得到 %T: %v", err, err)
	}
}
