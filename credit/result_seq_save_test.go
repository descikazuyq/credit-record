package credit

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// 本文件回归“保存时不得把两份已提交成绩写成共用同一正整数顺序编号”。正常
// 提交流程不可能产生这种冲突（序号由全局计数器递增分配，Load 也会把冲突
// 文件判为损坏），因此用例直接调用记录功能：先用正常流程登记并提交成绩，
// 再把两份修读的 ResultSeq 改成同一个正整数后请求 Save，聚焦“保存”这一路径：
//   - 两份不同修读（都已提交结果）共用同一正整数序号时，无论两份都通过、
//     一份通过一份未通过、还是两份都未通过，也无论它们是同一学生的重复修读、
//     同一学生不同要求的提交，还是不同学生名下编号相同的独立修读，Save 都
//     必须在写入前拒绝整次保存；
//   - 错误必须点名目标记录文件、重复的顺序编号以及冲突的两份修读，修读用
//     完整学生编号与修读编号共同说明；
//   - 冲突的两条记录不必相邻，夹杂的其他正常成绩与有效免修不能掩盖冲突；
//   - 拒绝后已有文件逐字节保留、仍可正常读取，目标原本不存在时不创建记录
//     文件、也不遗留临时文件；内存中的待保存记录保持提交时的内容，不删修读、
//     不调序号、不清成绩或免修历史；
//   - 尚未提交结果的多条选课共用序号 0 合法，已提交序号允许空缺，不同学生
//     共用修读编号但序号不同仍正常保存，保存后的学分与来源规则不变。

// seqSaveBaseStore 构造一份结构完整、引用齐全的记录：课程 c1、学生 s1、
// 要求 r1、两条选课 e1/e2（同一年学期的重复修读）、有效免修 w1 与一条
// 已拒绝免修 wbad，供用例提交成绩后改动序号。
func seqSaveBaseStore(t *testing.T) *Store {
	t.Helper()
	s := NewStore()
	mustCourse(t, s, "c1", "高等数学", 4)
	mustStudent(t, s, "s1")
	mustReq(t, s, "s1", "r1", "c1")
	mustEnroll(t, s, "s1", "r1", "2024春", "e1")
	mustEnroll(t, s, "s1", "r1", "2024春", "e2")
	if w, _, err := s.ApplyWaiver("s1", "r1", "w1", "竞赛获奖依据"); err != nil || w.Status != WaiverApproved {
		t.Fatalf("构造有效免修失败：%+v %v", w, err)
	}
	if w, _, err := s.ApplyWaiver("s1", "不存在的要求", "wbad", "旧依据"); err != nil ||
		w.Status != WaiverRejected {
		t.Fatalf("构造被拒绝免修失败：%+v %v", w, err)
	}
	return s
}

// assertDupSeqSaveRejected 是序号冲突用例的共同断言：Save 必须失败，错误
// 点名目标文件、重复序号与冲突的两份修读（完整学生编号 + 修读编号），并
// 返回可按类型检视的 duplicateResultSeqError。
func assertDupSeqSaveRejected(t *testing.T, s *Store, path string,
	seq int, student1, enr1, student2, enr2, label string) *duplicateResultSeqError {
	t.Helper()
	err := s.Save(path)
	if err == nil {
		t.Fatalf("%s：两份已提交修读共用顺序编号 %d 时应拒绝整次保存", label, seq)
	}
	var dup *duplicateResultSeqError
	if !errors.As(err, &dup) {
		t.Fatalf("%s：错误应是重复顺序编号错误，得到：%v", label, err)
	}
	if dup.seq != seq {
		t.Fatalf("%s：错误应指出重复序号 %d，得到 %d（%v）", label, seq, dup.seq, err)
	}
	msg := err.Error()
	for _, want := range []string{
		path,
		"重复",
		"顺序编号",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("%s：错误应包含 %q（点名目标文件与重复事实），得到：%v",
				label, want, err)
		}
	}
	// 两份冲突修读都要用完整学生编号与修读编号共同说明：同一学生时两处都是
	// 该学生编号；不同学生时必须能把两人名下的同号修读区分开。
	for _, want := range []string{
		"学生 " + student1 + " 的修读 " + enr1,
		"学生 " + student2 + " 的修读 " + enr2,
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("%s：错误应用完整学生编号与修读编号说明冲突修读（缺 %q），得到：%v",
				label, want, err)
		}
	}
	if !strings.Contains(msg, strconv.Itoa(seq)) {
		t.Fatalf("%s：错误应给出重复的顺序编号数字 %d，得到：%v", label, seq, err)
	}
	return dup
}

// TestSaveRejectsDuplicateResultSeqAcrossResultKinds 两份不同修读的已提交
// 成绩被改成同一正整数顺序编号时，无论通过/未通过如何组合都必须拒绝保存。
func TestSaveRejectsDuplicateResultSeqAcrossResultKinds(t *testing.T) {
	cases := []struct {
		label         string
		first, second Result
	}{
		{"两份都通过", Passed, Passed},
		{"一份通过一份未通过", Passed, Failed},
		{"一份未通过一份通过", Failed, Passed},
		{"两份都未通过", Failed, Failed},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			s := seqSaveBaseStore(t)
			if _, _, err := s.SubmitResult("s1", "e1", c.first); err != nil {
				t.Fatalf("提交 e1 失败：%v", err)
			}
			if _, _, err := s.SubmitResult("s1", "e2", c.second); err != nil {
				t.Fatalf("提交 e2 失败：%v", err)
			}
			// 模拟调用方把两份已提交成绩改成同一正整数顺序编号。
			s.Enrollment("s1", "e1").ResultSeq = 7
			s.Enrollment("s1", "e2").ResultSeq = 7

			path := filepath.Join(t.TempDir(), "records.json")
			assertDupSeqSaveRejected(t, s, path, 7, "s1", "e1", "s1", "e2", c.label)

			if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
				t.Fatalf("%s：目标文件原本不存在时不得创建，stat err=%v", c.label, statErr)
			}
			leftover, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".credit-*.tmp"))
			if len(leftover) != 0 {
				t.Fatalf("%s：不得遗留临时文件：%v", c.label, leftover)
			}

			// 内存记录保持提交时的内容：两份修读都在，结果与序号都不被改动，
			// 也不清除免修历史。
			e1 := s.Enrollment("s1", "e1")
			e2 := s.Enrollment("s1", "e2")
			if e1 == nil || e1.Result != c.first || e1.ResultSeq != 7 {
				t.Fatalf("%s：拒绝保存不得改动 e1，得到 %+v", c.label, e1)
			}
			if e2 == nil || e2.Result != c.second || e2.ResultSeq != 7 {
				t.Fatalf("%s：拒绝保存不得改动 e2，得到 %+v", c.label, e2)
			}
			if s.Waiver("s1", "w1") == nil || s.Waiver("s1", "wbad") == nil {
				t.Fatalf("%s：拒绝保存不得清除免修历史", c.label)
			}
		})
	}
}

// TestSaveRejectsDuplicateResultSeqAcrossStudents 两名学生各有一份编号为
// e1 的修读，两份成绩被改成同一顺序编号（题述的 7）时必须拒绝；错误要能用
// 完整学生编号把两人名下的同号修读区分开。不同要求、不同学期同样不能共用。
func TestSaveRejectsDuplicateResultSeqAcrossStudents(t *testing.T) {
	s := NewStore()
	mustCourse(t, s, "c1", "高等数学", 4)
	mustCourse(t, s, "c2", "线性代数", 3)
	mustStudent(t, s, "s1")
	mustStudent(t, s, "s2")
	// 两名学生各自名下都有编号 r1 的要求，但指向各自的课程。
	mustReq(t, s, "s1", "r1", "c1")
	mustReq(t, s, "s2", "r1", "c2")
	// 两名学生各自名下都有编号 e1 的修读（学期也相同）。
	mustEnroll(t, s, "s1", "r1", "2024春", "e1")
	mustEnroll(t, s, "s2", "r1", "2024春", "e1")
	if _, _, err := s.SubmitResult("s1", "e1", Passed); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SubmitResult("s2", "e1", Failed); err != nil {
		t.Fatal(err)
	}
	s.Enrollment("s1", "e1").ResultSeq = 7
	s.Enrollment("s2", "e1").ResultSeq = 7

	path := filepath.Join(t.TempDir(), "records.json")
	dup := assertDupSeqSaveRejected(t, s, path, 7, "s1", "e1", "s2", "e1",
		"两名学生各有一份编号 e1 的修读")
	// 类型化错误的两端也要各归本人，不能把两份同号修读当成同一份。
	pairs := map[[2]string]bool{
		{dup.firstStudent, dup.firstEnr}:   true,
		{dup.secondStudent, dup.secondEnr}: true,
	}
	if !pairs[[2]string{"s1", "e1"}] || !pairs[[2]string{"s2", "e1"}] {
		t.Fatalf("冲突两端应分别是 s1/e1 与 s2/e1，得到 %+v", dup)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("不得创建记录文件，stat err=%v", err)
	}
	leftover, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".credit-*.tmp"))
	if len(leftover) != 0 {
		t.Fatalf("不得遗留临时文件：%v", leftover)
	}

	// 两人的内存记录都保持提交时的内容。
	if e := s.Enrollment("s1", "e1"); e == nil || e.Result != Passed || e.ResultSeq != 7 ||
		e.ReqID != "r1" {
		t.Fatalf("s1 的修读不得被改动或删除：%+v", e)
	}
	if e := s.Enrollment("s2", "e1"); e == nil || e.Result != Failed || e.ResultSeq != 7 ||
		e.ReqID != "r1" {
		t.Fatalf("s2 的修读不得被改动或删除：%+v", e)
	}
}

// TestSaveDuplicateResultSeqNonAdjanticNotMasked 重复序号的两条记录不相邻、
// 中间夹着另一条正常成绩与有效免修时仍要拒绝；其他正常成绩或有效免修不能
// 掩盖冲突。
func TestSaveDuplicateResultSeqNonAdjanticNotMasked(t *testing.T) {
	s := NewStore()
	mustCourse(t, s, "c1", "高等数学", 4)
	mustStudent(t, s, "s1")
	mustReq(t, s, "s1", "r1", "c1")
	mustEnroll(t, s, "s1", "r1", "2024春", "e1")
	mustEnroll(t, s, "s1", "r1", "2024秋", "e2")
	mustEnroll(t, s, "s1", "r1", "2025春", "e3")
	// 提交次序 e1、e2、e3 分别拿到 1/2/3。
	for _, st := range []struct {
		id string
		r  Result
	}{{"e1", Passed}, {"e2", Failed}, {"e3", Passed}} {
		if _, _, err := s.SubmitResult("s1", st.id, st.r); err != nil {
			t.Fatalf("提交 %s 失败：%v", st.id, err)
		}
	}
	// 让不相邻的 e1 与 e3 共用序号 6，中间 e2 仍是正常的序号 2。
	s.Enrollment("s1", "e1").ResultSeq = 6
	s.Enrollment("s1", "e3").ResultSeq = 6

	path := filepath.Join(t.TempDir(), "records.json")
	assertDupSeqSaveRejected(t, s, path, 6, "s1", "e1", "s1", "e3",
		"重复序号不相邻且夹杂正常成绩")

	// 三条成绩与各自的结果都保留，序号不被替用户调整。
	if e := s.Enrollment("s1", "e1"); e == nil || e.Result != Passed || e.ResultSeq != 6 {
		t.Fatalf("e1 不应被改动：%+v", e)
	}
	if e := s.Enrollment("s1", "e2"); e == nil || e.Result != Failed || e.ResultSeq != 2 {
		t.Fatalf("中间的正常成绩 e2 不应被改动：%+v", e)
	}
	if e := s.Enrollment("s1", "e3"); e == nil || e.Result != Passed || e.ResultSeq != 6 {
		t.Fatalf("e3 不应被改动：%+v", e)
	}
	if n := len(s.Enrollments("s1")); n != 3 {
		t.Fatalf("拒绝保存不得删除任何修读，得到 %d 条", n)
	}
}

// TestSaveDuplicateResultSeqKeepsExistingFileUntouched 目标文件已存在且内容
// 完好时，含重复序号的保存必须失败：原文件逐字节保留、仍可正常读取核对，
// 待保存内存记录不被修改。
func TestSaveDuplicateResultSeqKeepsExistingFileUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "records.json")

	// 先落一份完好文件：s1 的 e1 通过、e2 选课。
	good := seqSaveBaseStore(t)
	if _, _, err := good.SubmitResult("s1", "e1", Passed); err != nil {
		t.Fatal(err)
	}
	if err := good.Save(path); err != nil {
		t.Fatalf("前置正常保存失败：%v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// 再构造一份两份成绩同序号的记录，尝试覆盖同一路径。
	bad := seqSaveBaseStore(t)
	if _, _, err := bad.SubmitResult("s1", "e1", Passed); err != nil {
		t.Fatal(err)
	}
	if _, _, err := bad.SubmitResult("s1", "e2", Failed); err != nil {
		t.Fatal(err)
	}
	bad.Enrollment("s1", "e1").ResultSeq = 9
	bad.Enrollment("s1", "e2").ResultSeq = 9
	assertDupSeqSaveRejected(t, bad, path, 9, "s1", "e1", "s1", "e2",
		"覆盖已有完好文件")

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("原文件应继续可读：%v", err)
	}
	if string(got) != string(raw) {
		t.Fatalf("拒绝保存后原文件必须逐字节保留\nwant=%q\n got=%q", raw, got)
	}
	leftover, _ := filepath.Glob(filepath.Join(dir, ".credit-*.tmp"))
	if len(leftover) != 0 {
		t.Fatalf("不得遗留临时文件：%v", leftover)
	}

	// 原文件仍可正常读取与核对：仍是 e1 通过带来的 4 学分，没有 e2 的成绩。
	loaded, existed, err := Load(path)
	if err != nil || !existed {
		t.Fatalf("原文件应仍可正常读取：existed=%v err=%v", existed, err)
	}
	rep := loaded.CheckStudent("s1")
	if rep.TotalCredits != 4 || len(rep.Unmet) != 0 {
		t.Fatalf("原文件学分结论应不变，得到 %+v", rep)
	}
	if e := loaded.Enrollment("s1", "e2"); e == nil || e.Result != Enrolled {
		t.Fatalf("原文件中的 e2 应仍是选课，得到 %+v", e)
	}

	// 内存中想保存的两份成绩不被改动。
	if e := bad.Enrollment("s1", "e1"); e == nil || e.Result != Passed || e.ResultSeq != 9 {
		t.Fatalf("待保存内存记录的 e1 不应被改动：%+v", e)
	}
	if e := bad.Enrollment("s1", "e2"); e == nil || e.Result != Failed || e.ResultSeq != 9 {
		t.Fatalf("待保存内存记录的 e2 不应被改动：%+v", e)
	}
}

// TestSaveLegalResultSeqPatternsAccepted 合法的序号形态必须继续正常保存：
// 多条选课共用序号 0、已提交序号存在空缺、两名学生共用修读编号但序号不同、
// 两份未通过使用不同序号。保存后学分与来源规则保持不变，并可重新读取。
func TestSaveLegalResultSeqPatternsAccepted(t *testing.T) {
	t.Run("多条选课序号0与已提交空缺并存", func(t *testing.T) {
		s := NewStore()
		mustCourse(t, s, "c1", "高等数学", 4)
		mustStudent(t, s, "s1")
		mustReq(t, s, "s1", "r1", "c1")
		mustEnroll(t, s, "s1", "r1", "2024春", "e1") // 选课，序号 0
		mustEnroll(t, s, "s1", "r1", "2024夏", "e2") // 选课，序号 0
		mustEnroll(t, s, "s1", "r1", "2024秋", "e3")
		mustEnroll(t, s, "s1", "r1", "2025春", "e4")
		if _, _, err := s.SubmitResult("s1", "e3", Failed); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.SubmitResult("s1", "e4", Passed); err != nil {
			t.Fatal(err)
		}
		// 已提交序号刻意留出空缺：未通过 1、通过 5（2/3/4 缺失）。计数器必须
		// 不小于已有最大序号（与真实记录一致），直接置为 5 表示中间序号空缺。
		s.Enrollment("s1", "e3").ResultSeq = 1
		s.Enrollment("s1", "e4").ResultSeq = 5
		s.nextResultSeq = 5

		path := filepath.Join(t.TempDir(), "records.json")
		if err := s.Save(path); err != nil {
			t.Fatalf("合法序号形态不应拒绝保存：%v", err)
		}
		loaded, _, err := Load(path)
		if err != nil {
			t.Fatalf("合法记录保存后应可重新读取：%v", err)
		}
		rep := loaded.CheckStudent("s1")
		if rep.TotalCredits != 4 || len(rep.Unmet) != 0 {
			t.Fatalf("通过的 e4 应带来一份 4 学分，得到 %+v", rep)
		}
		if rep.Requirements[0].Source != "enrollment" ||
			rep.Requirements[0].PassedEnrollmentID != "e4" {
			t.Fatalf("序号 1 的未通过不能成为来源，来源应是 e4，得到 %+v",
				rep.Requirements[0])
		}
		if n := len(loaded.Enrollments("s1")); n != 4 {
			t.Fatalf("四条修读都应保留，得到 %d 条", n)
		}
		for _, id := range []string{"e1", "e2"} {
			if e := loaded.Enrollment("s1", id); e == nil || e.Result != Enrolled || e.ResultSeq != 0 {
				t.Fatalf("选课 %s 应保持序号 0，得到 %+v", id, e)
			}
		}
	})

	t.Run("跨学生同修读编号不同序号", func(t *testing.T) {
		s := NewStore()
		mustCourse(t, s, "c1", "高等数学", 4)
		mustCourse(t, s, "c2", "线性代数", 3)
		mustStudent(t, s, "s1")
		mustStudent(t, s, "s2")
		mustReq(t, s, "s1", "r1", "c1")
		mustReq(t, s, "s2", "r1", "c2")
		mustEnroll(t, s, "s1", "r1", "2024春", "e1")
		mustEnroll(t, s, "s2", "r1", "2024春", "e1")
		if _, _, err := s.SubmitResult("s1", "e1", Passed); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.SubmitResult("s2", "e1", Passed); err != nil {
			t.Fatal(err)
		}
		// 序号不同（1 与 8，且 8 之前有空缺）：各自正常保存。
		s.Enrollment("s1", "e1").ResultSeq = 1
		s.Enrollment("s2", "e1").ResultSeq = 8
		s.nextResultSeq = 8

		path := filepath.Join(t.TempDir(), "records.json")
		if err := s.Save(path); err != nil {
			t.Fatalf("不同学生同号修读序号不同应正常保存：%v", err)
		}
		loaded, _, err := Load(path)
		if err != nil {
			t.Fatalf("保存后应可重新读取：%v", err)
		}
		if rep := loaded.CheckStudent("s1"); rep.TotalCredits != 4 ||
			rep.Requirements[0].PassedEnrollmentID != "e1" {
			t.Fatalf("s1 应凭本人 e1 得 4 学分，得到 %+v", rep)
		}
		if rep := loaded.CheckStudent("s2"); rep.TotalCredits != 3 ||
			rep.Requirements[0].PassedEnrollmentID != "e1" {
			t.Fatalf("s2 应凭本人 e1 得 3 学分，得到 %+v", rep)
		}
	})

	t.Run("两份未通过使用不同序号", func(t *testing.T) {
		// 不用 seqSaveBaseStore：它含一份满足 r1 的有效免修，会让学分非 0。
		s := NewStore()
		mustCourse(t, s, "c1", "高等数学", 4)
		mustStudent(t, s, "s1")
		mustReq(t, s, "s1", "r1", "c1")
		mustEnroll(t, s, "s1", "r1", "2024春", "e1")
		mustEnroll(t, s, "s1", "r1", "2024秋", "e2")
		if _, _, err := s.SubmitResult("s1", "e1", Failed); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.SubmitResult("s1", "e2", Failed); err != nil {
			t.Fatal(err)
		}
		s.Enrollment("s1", "e1").ResultSeq = 3
		s.Enrollment("s1", "e2").ResultSeq = 10
		s.nextResultSeq = 10

		path := filepath.Join(t.TempDir(), "records.json")
		if err := s.Save(path); err != nil {
			t.Fatalf("两份未通过序号不同应正常保存：%v", err)
		}
		loaded, _, err := Load(path)
		if err != nil {
			t.Fatalf("保存后应可重新读取：%v", err)
		}
		rep := loaded.CheckStudent("s1")
		if rep.TotalCredits != 0 || len(rep.Unmet) != 1 {
			t.Fatalf("两份未通过都不计学分，要求仍未满足，得到 %+v", rep)
		}
	})
}

// TestSaveDuplicateResultSeqDoesNotCreateTempOnReject 拒绝发生在序列化与
// 临时文件之前：目标原本不存在时，保存后目录中既没有记录文件，也没有任何
// .credit-*.tmp 临时文件。
func TestSaveDuplicateResultSeqDoesNotCreateTempOnReject(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "records.json")

	s := seqSaveBaseStore(t)
	if _, _, err := s.SubmitResult("s1", "e1", Passed); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SubmitResult("s1", "e2", Passed); err != nil {
		t.Fatal(err)
	}
	s.Enrollment("s1", "e1").ResultSeq = 2
	s.Enrollment("s1", "e2").ResultSeq = 2

	if err := s.Save(path); err == nil {
		t.Fatal("同序号保存应失败")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		var names []string
		for _, en := range entries {
			names = append(names, en.Name())
		}
		t.Fatalf("拒绝保存后目录应为空，却留下：%v", names)
	}
}
