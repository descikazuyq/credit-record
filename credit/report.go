package credit

import (
	"fmt"
	"math"
	"sort"
)

// RequirementStatus 是核对时单项要求的判定结果。
type RequirementStatus struct {
	Req       *Requirement
	Course    *Course
	Satisfied bool
	// Source 为 "waiver"（有效免修）或 "enrollment"（通过修读）或 ""（未满足）。
	Source string
	// WaiverID 是当前有效免修编号（有有效免修时以此说明学分来源）。
	WaiverID string
	// PassedEnrollmentID 是用于说明来源的通过修读编号：
	// 没有有效免修时取最先提交的通过记录。
	PassedEnrollmentID string
	// PassedEnrollmentIDs 是该要求全部通过修读编号（修读历史），按提交先后。
	PassedEnrollmentIDs []string
}

// RejectedWaiver 是免修历史中被拒绝的申请及原因。
type RejectedWaiver struct {
	Waiver *Waiver
	Reason string
}

// Report 是按学生核对学分的结果。
type Report struct {
	StudentID string
	Found     bool // 学生是否存在
	// TotalCredits 已满足要求获得的学分之和；同一课程的多项要求各计一次，
	// 同一要求无论通过多少次或同时有免修都只计一份。
	TotalCredits int
	// Overflow 为 true 表示按上述规则累加的总学分超出 int 可表示的最大值，
	// 此时 TotalCredits 不是有效结果（既不截断也不回绕），核对必须被拒绝，
	// 不能当作正常报告展示。
	Overflow     bool
	Requirements []RequirementStatus
	// Unmet 是未满足的要求编号（保持 Requirements 中的顺序）。
	Unmet []string
	// RejectedWaivers 是该学生被拒绝的免修申请及具体原因，按申请顺序。
	RejectedWaivers []RejectedWaiver
	// RevokedWaivers 是已撤销免修编号，按申请顺序。
	RevokedWaivers []string
}

// CheckStudent 按学生核对学分。查询没有记录的学生时 Found=false。
func (s *Store) CheckStudent(studentID string) Report {
	rep := Report{StudentID: studentID}
	if _, ok := s.studentByID[studentID]; !ok {
		return rep
	}
	rep.Found = true

	// 汇总每项要求的通过修读（按结果提交先后）。
	type seqEntry struct {
		id  string
		seq int
	}
	passOrder := map[string][]seqEntry{}
	for _, e := range s.enrollments {
		if e.StudentID != studentID || e.Result != Passed {
			continue
		}
		passOrder[e.ReqID] = append(passOrder[e.ReqID], seqEntry{e.ID, e.ResultSeq})
	}

	for _, r := range s.requirements {
		if r.StudentID != studentID {
			continue
		}
		st := RequirementStatus{Req: r, Course: s.courseByID[r.CourseID]}
		entries := passOrder[r.ID]
		sort.SliceStable(entries, func(i, j int) bool { return entries[i].seq < entries[j].seq })
		for _, en := range entries {
			st.PassedEnrollmentIDs = append(st.PassedEnrollmentIDs, en.id)
		}

		if w := s.validWaiver(studentID, r.ID); w != nil {
			// 有有效免修：以免修说明当前学分来源；修读历史同时保留。
			st.Satisfied = true
			st.Source = "waiver"
			st.WaiverID = w.ID
		} else if len(entries) > 0 {
			// 没有有效免修时以最先提交的通过记录说明来源。
			st.Satisfied = true
			st.Source = "enrollment"
			st.PassedEnrollmentID = entries[0].id
		}

		if st.Satisfied && st.Course != nil {
			// 各门课程学分本身合法，但累加结果可能超出 int 上限：
			// 一旦超限就标记 Overflow 并停止累加，绝不让总学分回绕成
			// 负数或停留在部分累加的中间值被当作有效结果。
			if rep.Overflow || st.Course.Credit > math.MaxInt-rep.TotalCredits {
				rep.Overflow = true
			} else {
				rep.TotalCredits += st.Course.Credit
			}
		} else {
			rep.Unmet = append(rep.Unmet, r.ID)
		}
		rep.Requirements = append(rep.Requirements, st)
	}

	for _, w := range s.waivers {
		if w.StudentID != studentID {
			continue
		}
		switch w.Status {
		case WaiverRejected:
			rep.RejectedWaivers = append(rep.RejectedWaivers,
				RejectedWaiver{Waiver: w, Reason: w.Reason})
		case WaiverRevoked:
			rep.RevokedWaivers = append(rep.RevokedWaivers, w.ID)
		}
	}
	return rep
}

// zhResult 将修读结果转为中文说明。
func zhResult(r Result) string {
	switch r {
	case Passed:
		return "通过"
	case Failed:
		return "未通过"
	default:
		return "选课"
	}
}

// String 以中文多行文本呈现核对结果。
func (rep Report) String() string {
	if !rep.Found {
		return fmt.Sprintf("学生 %s 不存在，没有任何记录", rep.StudentID)
	}
	var b []byte
	b = append(b, fmt.Sprintf("学生 %s 核对结果\n", rep.StudentID)...)
	b = append(b, fmt.Sprintf("总学分：%d\n", rep.TotalCredits)...)
	if len(rep.Requirements) == 0 {
		b = append(b, "课程要求：（无）\n"...)
	} else {
		b = append(b, "课程要求：\n"...)
		for _, st := range rep.Requirements {
			courseName := ""
			credit := 0
			if st.Course != nil {
				courseName = st.Course.Name
				credit = st.Course.Credit
			}
			if st.Satisfied {
				switch st.Source {
				case "waiver":
					b = append(b, fmt.Sprintf(
						"  - 要求 %s（课程 %s《%s》，%d 学分）：已满足，来源为有效免修 %s",
						st.Req.ID, st.Req.CourseID, courseName, credit, st.WaiverID)...)
					if len(st.PassedEnrollmentIDs) > 0 {
						b = append(b, fmt.Sprintf("；另有通过修读历史 %v，不重复计学分",
							st.PassedEnrollmentIDs)...)
					}
				case "enrollment":
					b = append(b, fmt.Sprintf(
						"  - 要求 %s（课程 %s《%s》，%d 学分）：已满足，来源为通过修读 %s",
						st.Req.ID, st.Req.CourseID, courseName, credit,
						st.PassedEnrollmentID)...)
					if len(st.PassedEnrollmentIDs) > 1 {
						b = append(b, fmt.Sprintf("（共通过 %d 次，仅计一次学分）",
							len(st.PassedEnrollmentIDs))...)
					}
				}
			} else {
				b = append(b, fmt.Sprintf(
					"  - 要求 %s（课程 %s《%s》，%d 学分）：未满足",
					st.Req.ID, st.Req.CourseID, courseName, credit)...)
			}
			b = append(b, '\n')
		}
	}
	if len(rep.Unmet) > 0 {
		b = append(b, fmt.Sprintf("未满足要求：%v\n", rep.Unmet)...)
	} else {
		b = append(b, "未满足要求：（无）\n"...)
	}
	if len(rep.RejectedWaivers) > 0 {
		b = append(b, "被拒绝的免修：\n"...)
		for _, rj := range rep.RejectedWaivers {
			b = append(b, fmt.Sprintf("  - 免修 %s（要求 %s，依据 %q）：%s\n",
				rj.Waiver.ID, rj.Waiver.ReqID, rj.Waiver.Basis, rj.Reason)...)
		}
	}
	if len(rep.RevokedWaivers) > 0 {
		b = append(b, fmt.Sprintf("已撤销免修：%v（原依据与撤销状态保留在免修历史中）\n",
			rep.RevokedWaivers)...)
	}
	return string(b)
}
