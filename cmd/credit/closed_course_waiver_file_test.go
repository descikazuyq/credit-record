package main

import (
	"fmt"
	"strings"
	"testing"
)

// 本文件从命令行入口回归“课程停开只限制新增修读，不取消学生已有的课程要求，
// 也不禁止凭有效依据申请免修”这一现有规则。停开不是免修的资格条件：免修
// 申请的受理只看目标要求是否属于该学生、依据是否含实际文字、该要求是否已有
// 有效免修，与课程开放状态无关。每条命令都重新打开记录文件，因此申请反馈、
// 学生记录（show）、学分核对（check）与课程列表之间的一致性同时覆盖落盘后
// 再加载的结果：
//   - 课程已停开、要求既没有通过修读也没有有效免修时，用未使用过的免修编号
//     和含实际文字的依据申请应当成功（退出码 0，说明免修有效）；show 能查到
//     属于该学生、指向该要求的申请，编号与依据完整保留，且不会为满足要求凭空
//     生成一份通过修读；check 把要求列为已满足、以新免修编号说明来源、计入
//     课程四学分并从未满足要求中移除；课程列表仍显示停开，新增修读仍被拒绝；
//   - 停开前已取得的通过修读不会挡住合法免修：申请后仍只计一份课程学分，
//     当前来源改为有效免修，原通过修读历史（要求、学期、通过结果）保留；
//   - 停开不放宽免修原有的拒绝规则：依据为空字符串或全部由空白字符（普通
//     空格与全角空格等混用）组成时退出码 1，申请内容与“免修依据为空”的原因
//     进入该学生的拒绝历史，拒绝说明不能误报课程停开，不产生有效免修；该要求
//     已有有效免修时再用新编号申请同样退出码 1，原因指出现有有效免修，原申请
//     继续生效。两种拒绝都在 show 与 check 中有据可查，不改变学分来源；
//   - 含实际文字的依据即使前后带空白（普通空格与全角空格）仍按原文接受，
//     show 中依据逐字符保留。

// setupCLIClosedCourseWithReq 建立一名学生、一门四学分课程及学生名下指向它的
// 一项要求，随后把课程停开；不登记任何修读与免修。
func setupCLIClosedCourseWithReq(t *testing.T, file string) {
	t.Helper()
	for _, st := range [][]string{
		{"student", "s1"},
		{"course", "c1", "高等数学", "4"},
		{"req", "s1", "r1", "c1"},
		{"course-close", "c1"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}
}

// TestCLIClosedCourseWaiverApprovedWithoutAnyEnrollment 课程停开、要求没有任何
// 修读记录时，用新免修编号与含实际文字的依据申请必须成功：退出码 0 并说明
// 免修有效；不得为满足要求自动生成一份通过修读；核对计四学分、以免修说明
// 来源并把要求移出未满足列表；课程列表仍显示停开、新增修读仍被拒绝。
func TestCLIClosedCourseWaiverApprovedWithoutAnyEnrollment(t *testing.T) {
	file := tempRecordFile(t)
	setupCLIClosedCourseWithReq(t, file)

	// 申请前核对：0 学分、r1 未满足，且学生名下没有任何修读记录。
	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：0") ||
		!strings.Contains(out, "未满足要求：[r1]") {
		t.Fatalf("停开后申请免修前应为 0 学分、r1 未满足，code=%d out=%q", code, out)
	}
	if show, _, _ := runCLI(t, file, "show", "s1"); !strings.Contains(show, "修读：（无）") {
		t.Fatalf("申请前不应有任何修读记录，out=%q", show)
	}

	// 停开课程上用未使用过的免修编号与含实际文字的依据申请：退出码 0，
	// 反馈说明免修有效并点到学生、要求、编号与依据。
	out, errText, code := runCLI(t, file, "waiver", "s1", "r1", "w1", "学科竞赛获奖")
	if code != 0 {
		t.Fatalf("停开课程不应禁止凭有效依据申请免修，code=%d out=%q err=%q",
			code, out, errText)
	}
	if !strings.Contains(out,
		`免修 w1 有效：学生 s1 的要求 r1 凭依据 "学科竞赛获奖" 满足，获得课程学分`) {
		t.Fatalf("应说明免修 w1 有效并完整保留学生、要求与依据，out=%q", out)
	}

	// 学生记录：申请属于 s1、指向 r1，编号与依据完整、状态有效；
	// 不能为满足要求自动补出一份通过修读——修读仍为（无）。
	show, _, code := runCLI(t, file, "show", "s1")
	if code != 0 {
		t.Fatalf("show 失败 code=%d out=%q", code, show)
	}
	if !strings.Contains(show,
		`免修 w1：要求 r1，依据 "学科竞赛获奖"，状态：有效`) {
		t.Fatalf("show 应能查到属于 s1、指向 r1 的有效申请且编号依据完整，out=%q", show)
	}
	if !strings.Contains(show, "修读：（无）") || strings.Contains(show, "结果：通过") {
		t.Fatalf("免修满足要求不能自动生成通过修读，out=%q", show)
	}

	// 核对：r1 已满足、以新免修编号说明来源、计四学分，并未满足列表移除；
	// 不能出现通过修读来源。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("核对失败 code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out,
			"要求 r1（课程 c1《高等数学》，4 学分）：已满足，来源为有效免修 w1") ||
		strings.Contains(out, "未满足要求：[") ||
		strings.Contains(out, "来源为通过修读") {
		t.Fatalf("r1 应由有效免修 w1 满足、计 4 学分且无未满足要求，out=%q", out)
	}

	// 课程列表仍显示停开，且停开对新增修读的限制不被免修放宽。
	if out, _, _ := runCLI(t, file, "list-courses"); !strings.Contains(out,
		"课程 c1《高等数学》4 学分，状态：停开") {
		t.Fatalf("免修生效后课程仍应显示停开，out=%q", out)
	}
	if out, errText, code := runCLI(t, file, "enroll", "s1", "r1", "2025春", "e1"); code != 1 ||
		!strings.Contains(errText, "课程 c1 已停开，不能新增修读") {
		t.Fatalf("免修生效后停开课程仍应禁止新增修读，code=%d out=%q err=%q",
			code, out, errText)
	}
	if show, _, _ := runCLI(t, file, "show", "s1"); !strings.Contains(show, "修读：（无）") {
		t.Fatalf("被拒绝的新增修读不能进入学生记录，out=%q", show)
	}
}

// TestCLIClosedCourseWaiverAfterPriorPassReplacesSourceKeepsHistory 停开前已
// 取得的通过结果不会挡住合法免修：申请后仍只计该课程一份学分，核对的当前
// 来源改为有效免修（不能因总学分没有增加而仍显示修读来源），同时原通过修读
// 的要求、学期与通过结果在学生记录与核对历史中保留。
func TestCLIClosedCourseWaiverAfterPriorPassReplacesSourceKeepsHistory(t *testing.T) {
	file := tempRecordFile(t)
	for _, st := range [][]string{
		{"student", "s1"},
		{"course", "c1", "高等数学", "4"},
		{"req", "s1", "r1", "c1"},
		{"enroll", "s1", "r1", "2024春", "e1"},
		{"pass", "s1", "e1"},
		{"course-close", "c1"},
	} {
		if _, errText, code := runCLI(t, file, st...); code != 0 {
			t.Fatalf("步骤 %v 应成功，code=%d err=%q", st, code, errText)
		}
	}

	// 免修申请前：停开前的通过已让 r1 满足、计 4 学分，来源是通过修读 e1。
	out, _, code := runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "已满足，来源为通过修读 e1") {
		t.Fatalf("申请前应已由停开前通过修读 e1 满足，code=%d out=%q", code, out)
	}

	// 已有通过修读时用新免修编号申请：停开与既有通过都不挡住合法免修。
	out, errText, code := runCLI(t, file, "waiver", "s1", "r1", "w1", "外校同层次课程成绩单")
	if code != 0 {
		t.Fatalf("停开前的通过结果不应挡住合法免修，code=%d out=%q err=%q",
			code, out, errText)
	}
	if !strings.Contains(out, "免修 w1 有效") {
		t.Fatalf("应说明免修 w1 有效，out=%q", out)
	}

	// 核对：总学分仍是 4（只计一份），当前来源改为有效免修 w1，并保留通过
	// 修读历史 e1、注明不重复计学分；不能再把修读写成当前来源。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("核对失败 code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "总学分：4") {
		t.Fatalf("通过修读与有效免修并存仍只计一份 4 学分，out=%q", out)
	}
	if !strings.Contains(out, "已满足，来源为有效免修 w1") ||
		!strings.Contains(out, "另有通过修读历史 [e1]，不重复计学分") ||
		strings.Contains(out, "来源为通过修读") ||
		strings.Contains(out, "未满足要求：[") {
		t.Fatalf("当前来源应为有效免修 w1 且保留 e1 通过历史、只计一份，out=%q", out)
	}

	// 学生记录保留原修读的要求、学期与通过结果，同时新免修状态为有效。
	show, _, code := runCLI(t, file, "show", "s1")
	if code != 0 {
		t.Fatalf("show 失败 code=%d out=%q", code, show)
	}
	if !strings.Contains(show, "修读 e1：要求 r1，学期 2024春，结果：通过") {
		t.Fatalf("原通过修读的要求、学期与通过结果必须保留，out=%q", show)
	}
	if !strings.Contains(show, `免修 w1：要求 r1，依据 "外校同层次课程成绩单"，状态：有效`) {
		t.Fatalf("学生记录应体现免修 w1 已生效，out=%q", show)
	}

	// 课程仍显示停开。
	if out, _, _ := runCLI(t, file, "list-courses"); !strings.Contains(out,
		"课程 c1《高等数学》4 学分，状态：停开") {
		t.Fatalf("免修生效后课程仍应停开，out=%q", out)
	}
}

// TestCLIClosedCourseWaiverBlankBasisStillRejected 停开不放宽免修的拒绝规则：
// 目标要求存在，但依据为空字符串或全部由空白字符（普通空格与全角空格等
// 混用）组成时退出码 1，申请内容与“免修依据为空”的原因进入该学生的拒绝
// 历史；拒绝说明不能误报课程停开，也不产生有效免修，要求仍未满足、学分为零。
func TestCLIClosedCourseWaiverBlankBasisStillRejected(t *testing.T) {
	blanks := map[string]string{
		"空字符串":        "",
		"普通空格":        "     ",
		"全角空格":        "　　　",
		"普通与全角空格混用":  "  　　  ",
		"空白与制表换行混用": " \t　 \n\r　 ",
	}
	for name, basis := range blanks {
		t.Run(name, func(t *testing.T) {
			file := tempRecordFile(t)
			setupCLIClosedCourseWithReq(t, file)

			out, errText, code := runCLI(t, file, "waiver", "s1", "r1", "w1", basis)
			if code != 1 {
				t.Fatalf("空白依据应按业务规则拒绝（退出码 1），code=%d out=%q err=%q",
					code, out, errText)
			}
			if !strings.Contains(out, "免修申请 w1 已拒绝") ||
				!strings.Contains(out, "免修依据为空") {
				t.Fatalf("应说明申请已拒绝且原因为免修依据为空，out=%q", out)
			}
			// 拒绝只能归因于依据为空，不能误报课程停开。
			if strings.Contains(out, "停开") || strings.Contains(errText, "停开") {
				t.Fatalf("空白依据的拒绝原因不能误报课程停开，out=%q err=%q", out, errText)
			}

			// show：申请内容（含原文空白依据）与缺少依据的原因进入拒绝历史，
			// 不出现有效免修。
			show, _, code := runCLI(t, file, "show", "s1")
			if code != 0 {
				t.Fatalf("show 失败 code=%d out=%q", code, show)
			}
			wantShow := fmt.Sprintf("免修 w1：要求 r1，依据 %q，状态：已拒绝（免修依据为空）", basis)
			if !strings.Contains(show, wantShow) || strings.Contains(show, "状态：有效") {
				t.Fatalf("show 应保留空白依据原文与拒绝原因、无有效免修\nwant contains %q\nout=%q",
					wantShow, show)
			}

			// check：拒绝记录有据可查，r1 仍未满足、0 学分，不能出现免修来源。
			out, _, code = runCLI(t, file, "check", "s1")
			if code != 0 {
				t.Fatalf("核对失败 code=%d out=%q", code, out)
			}
			wantRej := fmt.Sprintf("免修 w1（要求 r1，依据 %q）：免修依据为空", basis)
			if !strings.Contains(out, wantRej) ||
				!strings.Contains(out, "总学分：0") ||
				!strings.Contains(out, "未满足要求：[r1]") ||
				strings.Contains(out, "来源为有效免修") ||
				strings.Contains(out, "已满足") {
				t.Fatalf("空白依据申请应进拒绝历史且不满足要求、不得学分，out=%q", out)
			}

			// 课程停开状态不因拒绝改变。
			if out, _, _ := runCLI(t, file, "list-courses"); !strings.Contains(out,
				"课程 c1《高等数学》4 学分，状态：停开") {
				t.Fatalf("拒绝后课程应仍显示停开，out=%q", out)
			}
		})
	}
}

// TestCLIClosedCourseSecondValidWaiverForSameReqRejected 该要求已有有效免修时，
// 停开课程上再用新免修编号申请同一要求仍退出码 1：拒绝原因指出现有有效免修，
// 不能误报停开；原申请继续生效，核对仍以原免修说明来源、计四学分，新申请进
// 拒绝历史。
func TestCLIClosedCourseSecondValidWaiverForSameReqRejected(t *testing.T) {
	file := tempRecordFile(t)
	setupCLIClosedCourseWithReq(t, file)

	if out, errText, code := runCLI(t, file, "waiver", "s1", "r1", "w1", "学科竞赛获奖"); code != 0 {
		t.Fatalf("首份免修应生效，code=%d out=%q err=%q", code, out, errText)
	}

	// 用未使用过的新编号就同一要求再次申请：退出码 1，原因指出现有有效免修。
	out, errText, code := runCLI(t, file, "waiver", "s1", "r1", "w2", "转专业前同类课程已修")
	if code != 1 {
		t.Fatalf("已有有效免修时新申请应拒绝（退出码 1），code=%d out=%q err=%q",
			code, out, errText)
	}
	if !strings.Contains(out, "免修申请 w2 已拒绝") ||
		!strings.Contains(out, "该要求已有有效免修 w1") {
		t.Fatalf("拒绝原因应指出现有有效免修 w1，out=%q", out)
	}
	if strings.Contains(out, "停开") || strings.Contains(errText, "停开") {
		t.Fatalf("拒绝原因不能误报课程停开，out=%q err=%q", out, errText)
	}

	// 核对：原免修 w1 继续生效、计 4 学分，w2 带着依据与原因出现在拒绝列表，
	// 不能把 w2 当成来源。
	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 {
		t.Fatalf("核对失败 code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "已满足，来源为有效免修 w1") ||
		!strings.Contains(out,
			`免修 w2（要求 r1，依据 "转专业前同类课程已修"）：该要求已有有效免修 w1`) ||
		strings.Contains(out, "来源为有效免修 w2") ||
		strings.Contains(out, "未满足要求：[") {
		t.Fatalf("原免修 w1 应继续生效，w2 应进拒绝历史且不改变学分来源，out=%q", out)
	}

	// show：w1 状态仍为有效，w2 为已拒绝并保留原因；原历史不被清除。
	show, _, code := runCLI(t, file, "show", "s1")
	if code != 0 {
		t.Fatalf("show 失败 code=%d out=%q", code, show)
	}
	if !strings.Contains(show, `免修 w1：要求 r1，依据 "学科竞赛获奖"，状态：有效`) {
		t.Fatalf("原申请 w1 应继续有效，out=%q", show)
	}
	if !strings.Contains(show,
		`免修 w2：要求 r1，依据 "转专业前同类课程已修"，状态：已拒绝（该要求已有有效免修 w1）`) {
		t.Fatalf("w2 应保留依据与拒绝原因，out=%q", show)
	}

	if out, _, _ := runCLI(t, file, "list-courses"); !strings.Contains(out,
		"课程 c1《高等数学》4 学分，状态：停开") {
		t.Fatalf("拒绝后课程应仍显示停开，out=%q", out)
	}
}

// TestCLIClosedCourseWaiverBasisWithSurroundingWhitespaceAcceptedVerbatim 含
// 实际文字的依据即使前后混有普通空格与全角空格，仍按原文接受：停开课程上的
// 申请退出码 0、免修有效，show 中的依据逐字符保留，核对计四学分、以免修说明
// 来源，且不产生任何修读记录。
func TestCLIClosedCourseWaiverBasisWithSurroundingWhitespaceAcceptedVerbatim(t *testing.T) {
	file := tempRecordFile(t)
	setupCLIClosedCourseWithReq(t, file)

	// 前后均混有普通空格与全角空格，中间是实际文字。
	basis := " 　学科竞赛获奖证明　 "
	out, errText, code := runCLI(t, file, "waiver", "s1", "r1", "w1", basis)
	if code != 0 {
		t.Fatalf("含实际文字的带空白依据应被接受，code=%d out=%q err=%q",
			code, out, errText)
	}
	if !strings.Contains(out, "免修 w1 有效") || !strings.Contains(out, fmt.Sprintf("%q", basis)) {
		t.Fatalf("应说明免修有效并保留依据原文，out=%q", out)
	}

	show, _, code := runCLI(t, file, "show", "s1")
	if code != 0 {
		t.Fatalf("show 失败 code=%d out=%q", code, show)
	}
	wantLine := fmt.Sprintf("免修 w1：要求 r1，依据 %q，状态：有效", basis)
	if !strings.Contains(show, wantLine) {
		t.Fatalf("带前后空白的依据必须按原文保留\nwant contains %q\nout=%q", wantLine, show)
	}
	if !strings.Contains(show, "修读：（无）") {
		t.Fatalf("免修不能自动生成修读记录，out=%q", show)
	}

	out, _, code = runCLI(t, file, "check", "s1")
	if code != 0 || !strings.Contains(out, "总学分：4") ||
		!strings.Contains(out, "已满足，来源为有效免修 w1") ||
		strings.Contains(out, "未满足要求：[") {
		t.Fatalf("带空白依据的有效免修应正常计 4 学分并满足 r1，code=%d out=%q",
			code, out)
	}
}
