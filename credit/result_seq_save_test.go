package credit

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件回归“保存时不得把两份不同修读的已提交成绩写成共用同一个正整数顺序
// 编号”。正常提交流程由计数器递增发号，冲突只会出现在“程序取得记录后直接
// 修改修读的 ResultSeq”这条路径上，因此用例先经公开登记/提交接口建立结构
// 完整的记录，再直接改成绩顺序编号，聚焦 Save 这一路径：
//   - 两份不同修读的已提交成绩共用同一正整数编号时（两条都通过、一通过一
//     未通过、两条都未通过；同一学生的重复修读、同一学生不同要求、不同学生
//     的同号修读；记录不相邻、夹杂其他正常成绩或有效免修），整次保存必须
//     拒绝：错误点名目标文件、重复编号与冲突双方（完整学生编号+修读编号）；
//   - 目标文件原本不存在时不创建记录文件，也不残留临时文件；已有文件的全部
//     字节原样保留、仍可正常读取与核对；
//   - 待保存的内存记录保持提交时的内容：不删修读、不改编号、不替用户重排、
//     不清成绩或免修历史；
//   - 多条选课沿用编号 0 不冲突；已提交编号允许空缺；不同学生同号修读但
//     编号不同时正常保存，学分与来源规则不变。

// saveSeqTwoStudentStore 建立两名学生、两门课程、各自要求 r1，以及可由用例
// 自行提交/改号的同号修读 e1（另加一条同学生的第二条修读 e2）。
func saveSeqTwoStudentStore(t *testing.T) *Store {
	t.Helper()
	s := NewStore()
	mustStudent(t, s, "s1")
	mustStudent(t, s, "s2")
	mustCourse(t, s, "c1", "高等数学", 4)
	mustCourse(t, s, "c2", "线性代数", 3)
	mustReq(t, s, "s1", "r1", "c1")
	mustReq(t, s, "s2", "r1", "c2")
	mustReq(t, s, "s1", "r2", "c2")
	mustEnroll(t, s, "s1", "r1", "2024春", "e1")
	mustEnroll(t, s, "s2", "r1", "2024春", "e1")
	mustEnroll(t, s, "s1", "r1", "2024秋", "e2")
	mustEnroll(t, s, "s1", "r2", "2025春", "e9")
	return s
}

// assertSaveDupSeqError 断言保存错误点名目标文件、重复顺序编号与冲突双方
// （完整学生编号 + 修读编号），并返回解析后的类型化错误。
func assertSaveDupSeqError(t *testing.T, err error, path string, seq int,
	student1, enr1, student2, enr2 string) *duplicateResultSeqError {
	t.Helper()
	if err == nil {
		t.Fatalf("顺序编号 %d 被两份修读共用时必须拒绝保存", seq)
	}
	var dse *duplicateResultSeqError
	if !errors.As(err, &dse) {
		t.Fatalf("应返回 *duplicateResultSeqError，得到 %T：%v", err, err)
	}
	if dse.seq != seq ||
		dse.firstStudent != student1 || dse.firstEnr != enr1 ||
		dse.secondStudent != student2 || dse.secondEnr != enr2 {
		t.Fatalf("类型化错误定位不符：seq=%d 第一=(%s,%s) 第二=(%s,%s)，得到 %+v",
			seq, student1, enr1, student2, enr2, dse)
	}
	msg := err.Error()
	for _, want := range []string{
		path,
		fmt.Sprintf("顺序编号 %d", seq),
		student1, enr1,
		student2, enr2,
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("错误应点名目标文件、重复编号 %d 与冲突双方（缺 %q），得到：%v",
				seq, want, err)
		}
	}
	return dse
}

// TestSaveRejectsDuplicateResultSeq 覆盖各类共用正整数顺序编号的组合：
// 结果组合、归属关系、编号/学期异同、排列位置都不能改变拒绝结论。
func TestSaveRejectsDuplicateResultSeq(t *testing.T) {
	t.Run("两条通过_同一学生重复修读_同号7", func(t *testing.T) {
		s := saveSeqTwoStudentStore(t)
		if _, _, err := s.SubmitResult("s1", "e1", Passed); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.SubmitResult("s1", "e2", Failed); err != nil {
			t.Fatal(err)
		}
		// 程序取得记录后把两份成绩改成同一正整数顺序编号。
		s.Enrollment("s1", "e1").ResultSeq = 7
		s.Enrollment("s1", "e2").ResultSeq = 7
		s.Enrollment("s1", "e2").Result = Passed

		path := filepath.Join(t.TempDir(), "records.json")
		err := s.Save(path)
		assertSaveDupSeqError(t, err, path, 7, "s1", "e1", "s1", "e2")
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("拒绝保存后不得创建记录文件，stat err=%v", statErr)
		}
		leftover, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".credit-*.tmp"))
		if len(leftover) != 0 {
			t.Fatalf("拒绝保存后不得残留临时文件：%v", leftover)
		}
	})

	t.Run("一条通过一条未通过", func(t *testing.T) {
		s := saveSeqTwoStudentStore(t)
		if _, _, err := s.SubmitResult("s1", "e1", Passed); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.SubmitResult("s1", "e2", Failed); err != nil {
			t.Fatal(err)
		}
		s.Enrollment("s1", "e1").ResultSeq = 5
		s.Enrollment("s1", "e2").ResultSeq = 5

		path := filepath.Join(t.TempDir(), "records.json")
		err := s.Save(path)
		assertSaveDupSeqError(t, err, path, 5, "s1", "e1", "s1", "e2")
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("拒绝保存后不得创建记录文件，stat err=%v", statErr)
		}
	})

	t.Run("两条都未通过", func(t *testing.T) {
		s := saveSeqTwoStudentStore(t)
		if _, _, err := s.SubmitResult("s1", "e1", Failed); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.SubmitResult("s1", "e2", Failed); err != nil {
			t.Fatal(err)
		}
		s.Enrollment("s1", "e1").ResultSeq = 9
		s.Enrollment("s1", "e2").ResultSeq = 9

		path := filepath.Join(t.TempDir(), "records.json")
		err := s.Save(path)
		assertSaveDupSeqError(t, err, path, 9, "s1", "e1", "s1", "e2")
	})

	t.Run("同一学生不同要求的独立提交", func(t *testing.T) {
		s := saveSeqTwoStudentStore(t)
		if _, _, err := s.SubmitResult("s1", "e1", Passed); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.SubmitResult("s1", "e9", Passed); err != nil {
			t.Fatal(err)
		}
		s.Enrollment("s1", "e1").ResultSeq = 4
		s.Enrollment("s1", "e9").ResultSeq = 4

		path := filepath.Join(t.TempDir(), "records.json")
		err := s.Save(path)
		assertSaveDupSeqError(t, err, path, 4, "s1", "e1", "s1", "e9")
	})

	t.Run("不同学生的同号修读e1_题目示例", func(t *testing.T) {
		// 两名学生各有一份编号为 e1 的修读，两份成绩被改成顺序编号 7。
		s := saveSeqTwoStudentStore(t)
		if _, _, err := s.SubmitResult("s1", "e1", Passed); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.SubmitResult("s2", "e1", Failed); err != nil {
			t.Fatal(err)
		}
		s.Enrollment("s1", "e1").ResultSeq = 7
		s.Enrollment("s2", "e1").ResultSeq = 7

		path := filepath.Join(t.TempDir(), "records.json")
		err := s.Save(path)
		// 错误必须用完整学生编号加修读编号说明双方，才能区分两名学生名下的同号 e1。
		dse := assertSaveDupSeqError(t, err, path, 7, "s1", "e1", "s2", "e1")
		if strings.Count(err.Error(), "e1") < 2 {
			t.Fatalf("冲突双方各有一份 e1，错误应分别点明，得到：%v", err)
		}
		if dse.firstStudent == dse.secondStudent {
			t.Fatalf("该用例冲突双方应分属两名学生，得到 %+v", dse)
		}
	})

	t.Run("冲突记录不相邻_夹杂其他提交与有效免修", func(t *testing.T) {
		s := saveSeqTwoStudentStore(t)
		// 记录顺序：e1(改 6)、s2/e1、e2、e9。
		if _, _, err := s.SubmitResult("s1", "e1", Passed); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.SubmitResult("s2", "e1", Passed); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.SubmitResult("s1", "e2", Failed); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.SubmitResult("s1", "e9", Failed); err != nil {
			t.Fatal(err)
		}
		// e1 与 e9 改成同号，二者之间隔着 s2/e1 与 e2 两条正常成绩。
		s.Enrollment("s1", "e1").ResultSeq = 6
		s.Enrollment("s1", "e9").ResultSeq = 6
		// 中间再放一份有效免修，正常记录不能掩盖冲突。
		if w, _, err := s.ApplyWaiver("s2", "r1", "w1", "竞赛获奖"); err != nil ||
			w.Status != WaiverApproved {
			t.Fatalf("有效免修构造失败：%+v %v", w, err)
		}

		path := filepath.Join(t.TempDir(), "records.json")
		err := s.Save(path)
		assertSaveDupSeqError(t, err, path, 6, "s1", "e1", "s1", "e9")
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("拒绝保存后不得创建记录文件，stat err=%v", statErr)
		}
	})
}

// TestSaveDuplicateResultSeqKeepsExistingFileUntouched 目标文件已存在且内容
// 完好时，含顺序编号冲突的保存必须失败，原文件逐字节保留、仍可读取核对；
// 冲突的内存记录保持提交时的内容。
func TestSaveDuplicateResultSeqKeepsExistingFileUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "records.json")

	// 先落一份完好文件：s1 的 e1 通过（顺序编号 1）、e2 未通过（顺序编号 2）。
	good := saveSeqTwoStudentStore(t)
	if _, _, err := good.SubmitResult("s1", "e1", Passed); err != nil {
		t.Fatal(err)
	}
	if _, _, err := good.SubmitResult("s1", "e2", Failed); err != nil {
		t.Fatal(err)
	}
	if err := good.Save(path); err != nil {
		t.Fatalf("前置正常保存失败：%v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// 另一份记录把 s1/e1 与 s2/e1 的成绩都改成顺序编号 1，尝试覆盖同一路径。
	bad := saveSeqTwoStudentStore(t)
	if _, _, err := bad.SubmitResult("s1", "e1", Passed); err != nil {
		t.Fatal(err)
	}
	if _, _, err := bad.SubmitResult("s2", "e1", Failed); err != nil {
		t.Fatal(err)
	}
	bad.Enrollment("s1", "e1").ResultSeq = 1
	bad.Enrollment("s2", "e1").ResultSeq = 1
	saveErr := bad.Save(path)
	assertSaveDupSeqError(t, saveErr, path, 1, "s1", "e1", "s2", "e1")

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("原文件应继续可读：%v", err)
	}
	if string(got) != string(raw) {
		t.Fatalf("拒绝保存后已有文件必须逐字节保持原样\nwant=%q\n got=%q", raw, got)
	}
	leftover, _ := filepath.Glob(filepath.Join(dir, ".credit-*.tmp"))
	if len(leftover) != 0 {
		t.Fatalf("拒绝保存后不得残留临时文件：%v", leftover)
	}

	// 原文件仍可正常读取与核对：s1 凭 e1 得 4 学分，文件中没有 s2 的成绩。
	loaded, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("原文件应仍可正常读取：existed=%v err=%v", existed, err)
	}
	if rep := loaded.CheckStudent("s1"); rep.TotalCredits != 4 ||
		rep.Requirements[0].PassedEnrollmentID != "e1" {
		t.Fatalf("原文件核对结果应保持不变，得到 %+v", rep)
	}
	if e := loaded.Enrollment("s2", "e1"); e == nil || e.Result != Enrolled {
		t.Fatalf("原文件中 s2 的 e1 应仍是未提交的选课，坏记录不得落盘，得到 %+v", e)
	}

	// 内存中的冲突记录保持提交时的内容：不删修读、不调顺序、不清成绩。
	e1 := bad.Enrollment("s1", "e1")
	e2 := bad.Enrollment("s2", "e1")
	if e1 == nil || e1.Result != Passed || e1.ResultSeq != 1 {
		t.Fatalf("s1 的 e1 应保持通过、顺序编号 1，得到 %+v", e1)
	}
	if e2 == nil || e2.Result != Failed || e2.ResultSeq != 1 {
		t.Fatalf("s2 的 e1 应保持未通过、顺序编号 1，得到 %+v", e2)
	}
	if n := len(bad.Enrollments("s1")); n != 3 {
		t.Fatalf("不得删除 s1 的任何修读，得到 %d 条", n)
	}
	if n := len(bad.Enrollments("s2")); n != 1 {
		t.Fatalf("不得删除 s2 的修读，得到 %d 条", n)
	}

	// 由调用方自行消除冲突后（而不是保存功能代为调整），同一路径即可正常保存。
	e2.ResultSeq = 3
	bad.nextResultSeq = 3 // 计数器随调用方改写后的最大编号保持一致（本检查不代劳）。
	if err := bad.Save(path); err != nil {
		t.Fatalf("调用方消除冲突后应能正常保存：%v", err)
	}
	reloaded, _, err := Load(path)
	if err != nil {
		t.Fatalf("消除冲突后的文件应可正常读取：%v", err)
	}
	if reloaded.Enrollment("s2", "e1").ResultSeq != 3 {
		t.Fatal("消除冲突后的修改应原样落盘")
	}
}

// TestSaveDuplicateResultSeqDoesNotMutateMemory 拒绝保存不得改动待保存内存
// 记录的任何内容：修读、成绩、顺序编号、计数器、免修历史都保持提交时的样子。
func TestSaveDuplicateResultSeqDoesNotMutateMemory(t *testing.T) {
	s := saveSeqTwoStudentStore(t)
	if _, _, err := s.SubmitResult("s1", "e1", Passed); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SubmitResult("s2", "e1", Failed); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ApplyWaiver("s1", "r2", "w1", "竞赛获奖"); err != nil {
		t.Fatal(err)
	}
	if w, _, err := s.ApplyWaiver("s1", "没有的要求", "wbad", "旧依据"); err != nil ||
		w.Status != WaiverRejected {
		t.Fatalf("被拒绝免修构造失败：%+v %v", w, err)
	}
	before := s.nextResultSeq
	s.Enrollment("s1", "e1").ResultSeq = 7
	s.Enrollment("s2", "e1").ResultSeq = 7

	path := filepath.Join(t.TempDir(), "records.json")
	if err := s.Save(path); err == nil {
		t.Fatal("共用顺序编号时必须拒绝保存")
	}

	// 冲突双方原样保留，其他成绩、选课、计数器与全部免修历史也都不动。
	if e := s.Enrollment("s1", "e1"); e == nil || e.Result != Passed || e.ResultSeq != 7 {
		t.Fatalf("s1 的 e1 不得被改动，得到 %+v", e)
	}
	if e := s.Enrollment("s2", "e1"); e == nil || e.Result != Failed || e.ResultSeq != 7 {
		t.Fatalf("s2 的 e1 不得被改动，得到 %+v", e)
	}
	if s.nextResultSeq != before {
		t.Fatalf("顺序编号计数器不得被改动：before=%d after=%d", before, s.nextResultSeq)
	}
	if e := s.Enrollment("s1", "e2"); e == nil || e.Result != Enrolled || e.ResultSeq != 0 {
		t.Fatalf("未提交的 e2 应保持选课、编号 0，得到 %+v", e)
	}
	if w := s.Waiver("s1", "w1"); w == nil || w.Status != WaiverApproved {
		t.Fatalf("有效免修不得被清除：%+v", w)
	}
	if w := s.Waiver("s1", "wbad"); w == nil || w.Status != WaiverRejected {
		t.Fatalf("被拒绝免修历史不得被清除：%+v", w)
	}
	if n := len(s.enrollments); n != 4 {
		t.Fatalf("不得删除任何修读记录，得到 %d 条", n)
	}
}

// TestSaveLegalResultSeqArrangementsAccepted 合法的编号安排继续正常保存，
// 学分与来源规则保持不变：
//   - 多条尚未提交结果的选课共用编号 0；
//   - 已提交编号有空缺（不连续）；
//   - 不同学生共用修读编号但顺序编号不同，来源各归本人；
//   - 同一要求多次通过只计一份学分，仍按最先提交（编号最小）的通过记录说明来源。
func TestSaveLegalResultSeqArrangementsAccepted(t *testing.T) {
	t.Run("多条选课零编号与跳号成绩并存", func(t *testing.T) {
		s := saveSeqTwoStudentStore(t)
		// e1 未通过（1）、e9 通过（5，跳号）；e2 与 s2/e1 仍是选课（0）。
		if _, _, err := s.SubmitResult("s1", "e1", Failed); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.SubmitResult("s1", "e9", Passed); err != nil {
			t.Fatal(err)
		}
		s.Enrollment("s1", "e9").ResultSeq = 5 // 2/3/4 空缺，合法。
		s.nextResultSeq = 5

		path := filepath.Join(t.TempDir(), "records.json")
		if err := s.Save(path); err != nil {
			t.Fatalf("零编号选课并存、已提交编号跳号时应正常保存：%v", err)
		}
		loaded, existed, err := Load(path)
		if err != nil || !existed {
			t.Fatalf("合法记录保存后应可读取：existed=%v err=%v", existed, err)
		}
		// r2 由编号 5 的通过修读满足，3 学分；r1 只有未通过与选课，未满足。
		rep := loaded.CheckStudent("s1")
		if rep.TotalCredits != 3 {
			t.Fatalf("应只凭 e9 的 3 学分课程计 3 学分，得到 %d", rep.TotalCredits)
		}
		var r2 *RequirementStatus
		for i := range rep.Requirements {
			if rep.Requirements[i].Req.ID == "r2" {
				r2 = &rep.Requirements[i]
			}
		}
		if r2 == nil || !r2.Satisfied || r2.PassedEnrollmentID != "e9" {
			t.Fatalf("r2 应由 e9 满足，得到 %+v", r2)
		}
		if e := loaded.Enrollment("s1", "e2"); e == nil || e.Result != Enrolled || e.ResultSeq != 0 {
			t.Fatalf("选课 e2 应保持编号 0，得到 %+v", e)
		}
	})

	t.Run("不同学生同号修读编号不同_正常保存且来源各归本人", func(t *testing.T) {
		s := saveSeqTwoStudentStore(t)
		if _, _, err := s.SubmitResult("s1", "e1", Passed); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.SubmitResult("s2", "e1", Failed); err != nil {
			t.Fatal(err)
		}
		// s1 序号 1（4 学分课程），s2 序号 2（3 学分课程，未通过）。

		path := filepath.Join(t.TempDir(), "records.json")
		if err := s.Save(path); err != nil {
			t.Fatalf("跨学生同号修读、顺序编号不同应正常保存：%v", err)
		}
		loaded, _, err := Load(path)
		if err != nil {
			t.Fatalf("合法记录保存后应可读取：%v", err)
		}
		if rep := loaded.CheckStudent("s1"); rep.TotalCredits != 4 ||
			rep.Requirements[0].PassedEnrollmentID != "e1" {
			t.Fatalf("s1 应凭本人 e1 得 4 学分，得到 %+v", rep.Requirements[0])
		}
		if rep := loaded.CheckStudent("s2"); rep.TotalCredits != 0 ||
			len(rep.Unmet) != 1 || rep.Unmet[0] != "r1" {
			t.Fatalf("s2 的 e1 未通过，应 0 学分未满足，得到 %+v", rep)
		}
		if loaded.Enrollment("s1", "e1") == loaded.Enrollment("s2", "e1") {
			t.Fatal("两名学生的同号修读应仍是两条独立记录")
		}
	})

	t.Run("同一要求多次通过只计一份_按最小编号说明来源", func(t *testing.T) {
		s := NewStore()
		mustStudent(t, s, "s1")
		mustCourse(t, s, "c1", "高等数学", 4)
		mustReq(t, s, "s1", "r1", "c1")
		mustEnroll(t, s, "s1", "r1", "2024春", "e1")
		mustEnroll(t, s, "s1", "r1", "2024秋", "e2")
		if _, _, err := s.SubmitResult("s1", "e1", Passed); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.SubmitResult("s1", "e2", Passed); err != nil {
			t.Fatal(err)
		}
		// 重排为带空缺的编号：e2 编号 2、e1 编号 5，来源仍应是编号较小的 e2。
		s.Enrollment("s1", "e2").ResultSeq = 2
		s.Enrollment("s1", "e1").ResultSeq = 5
		s.nextResultSeq = 9

		path := filepath.Join(t.TempDir(), "records.json")
		if err := s.Save(path); err != nil {
			t.Fatalf("编号各不相同的两次通过应正常保存：%v", err)
		}
		loaded, _, err := Load(path)
		if err != nil {
			t.Fatalf("合法记录保存后应可读取：%v", err)
		}
		rep := loaded.CheckStudent("s1")
		if rep.TotalCredits != 4 || len(rep.Unmet) != 0 {
			t.Fatalf("同一要求多次通过只计一份 4 学分，得到学分=%d 未满足=%v",
				rep.TotalCredits, rep.Unmet)
		}
		st := rep.Requirements[0]
		if st.PassedEnrollmentID != "e2" || len(st.PassedEnrollmentIDs) != 2 {
			t.Fatalf("应以编号较小的通过修读 e2 说明来源且两次通过都保留，得到 %+v", st)
		}
	})
}
