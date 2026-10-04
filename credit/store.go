// Package credit 实现本地课程学分与免修的登记、持久化与核对。
//
// 基本对象：
//   - 学生（Student）：以唯一编号登记；
//   - 课程（Course）：唯一编号、名称、正整数学分，初始开放，可停开/恢复；
//   - 课程要求（Requirement）：属于某名学生，编号在该学生名下唯一，指向一门课程；
//   - 修读（Enrollment）：属于某名学生的一项要求，记录学期与编号，结果为
//     选课/通过/未通过；
//   - 免修（Waiver）：明确指向该学生的一项要求，带编号与非空依据，状态为
//     有效/已拒绝/已撤销。
//
// 所有“重复登记相同内容”的操作都幂等：返回原记录、不新增数据；内容冲突的
// 重复请求一律拒绝并保留原记录。
package credit

import (
	"errors"
	"fmt"
	"strings"
)

// Result 是修读结果。
type Result string

const (
	// Enrolled 已选课、尚未有结果，不计学分。
	Enrolled Result = "enrolled"
	// Passed 通过。
	Passed Result = "passed"
	// Failed 未通过，不计学分。
	Failed Result = "failed"
)

// WaiverStatus 是免修申请的状态。
type WaiverStatus string

const (
	// WaiverApproved 有效免修：满足对应要求并获得课程学分。
	WaiverApproved WaiverStatus = "approved"
	// WaiverRejected 已拒绝：申请内容与拒绝原因保留在免修历史中。
	WaiverRejected WaiverStatus = "rejected"
	// WaiverRevoked 已撤销：原依据保留，要求按修读情况重新判定。
	WaiverRevoked WaiverStatus = "revoked"
)

// Course 课程：编号唯一，学分为正整数。
type Course struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Credit int    `json:"credit"`
	Open   bool   `json:"open"`
}

// Student 学生：只有唯一编号。
type Student struct {
	ID string `json:"id"`
}

// Requirement 课程要求：在所属学生名下编号唯一，指向一门已登记课程。
type Requirement struct {
	ID        string `json:"id"`
	StudentID string `json:"student"`
	CourseID  string `json:"course"`
}

// Enrollment 修读：属于某名学生的一项要求，编号在该学生名下唯一。
type Enrollment struct {
	ID        string `json:"id"`
	StudentID string `json:"student"`
	ReqID     string `json:"req"`
	Term      string `json:"term"`
	Result    Result `json:"result"`
	// ResultSeq 是首次提交结果的先后序号，用于确定“最先提交的通过记录”。
	ResultSeq int `json:"resultSeq,omitempty"`
}

// Waiver 免修申请。被拒绝或撤销后记录仍保留在免修历史中。
type Waiver struct {
	ID        string       `json:"id"`
	StudentID string       `json:"student"`
	ReqID     string       `json:"req"`
	Basis     string       `json:"basis"`
	Status    WaiverStatus `json:"status"`
	// Reason 记录拒绝原因或撤销原因。
	Reason string `json:"reason,omitempty"`
}

// Store 是全部记录的内存集合，零值即可用。
type Store struct {
	courses      []*Course
	students     []*Student
	requirements []*Requirement
	enrollments  []*Enrollment
	waivers      []*Waiver

	// 归属索引一律按字段逐一匹配，绝不能把学生编号与要求编号拼成一个
	// 字符串再比较：编号是任意字符串，本身可能含有空字符（U+0000），
	// 用任何分隔符拼接都会让 (学生 "s", 要求 "x\x00r") 与 (学生 "s\x00x",
	// 要求 "r") 这类属于不同学生的两项撞成同一键，造成重复误判或跨学生
	// 借用要求。
	courseByID    map[string]*Course
	studentByID   map[string]*Student
	reqByKey      map[ownerKey]*Requirement
	reqByDup      map[ownerKey]string // (学生, 课程) -> 要求编号
	enrByKey      map[ownerKey]*Enrollment
	waiverByKey   map[ownerKey]*Waiver
	nextResultSeq int
	// dirty 记录自加载以来是否发生过需要落盘的变更（新建被拒绝的免修也算）。
	dirty bool
}

// ownerKey 是“某学生名下某项记录”的归属键，两个字段分别按原文比较。
// 用结构体而非拼接字符串，使含空字符的编号也不会跨字段错位碰撞；
// 编号原文始终保留，不做删除、转义或改写。
type ownerKey struct {
	student string
	id      string
}

func newStore() *Store {
	s := &Store{}
	s.resetIndexes()
	return s
}

func (s *Store) resetIndexes() {
	s.courseByID = map[string]*Course{}
	s.studentByID = map[string]*Student{}
	s.reqByKey = map[ownerKey]*Requirement{}
	s.reqByDup = map[ownerKey]string{}
	s.enrByKey = map[ownerKey]*Enrollment{}
	s.waiverByKey = map[ownerKey]*Waiver{}
}

// NewStore 返回空记录集。
func NewStore() *Store { return newStore() }

// Dirty 报告自加载以来是否发生过需要落盘的变更。
// 被拒绝但要保留在免修历史中的申请也计为变更；纯幂等返回与只读核对不计。
func (s *Store) Dirty() bool { return s.dirty }

// ---------- 查询 ----------

// Course 按编号返回课程；不存在返回 nil。
func (s *Store) Course(id string) *Course { return s.courseByID[id] }

// Courses 按登记顺序返回全部课程。
func (s *Store) Courses() []*Course { return append([]*Course(nil), s.courses...) }

// Student 按编号返回学生；不存在返回 nil。
func (s *Store) Student(id string) *Student { return s.studentByID[id] }

// Students 按登记顺序返回全部学生。
func (s *Store) Students() []*Student { return append([]*Student(nil), s.students...) }

// Requirement 返回某学生名下的要求；不存在（包括要求属于其他学生）返回 nil。
func (s *Store) Requirement(student, reqID string) *Requirement {
	return s.reqByKey[ownerKey{student, reqID}]
}

// Requirements 返回某学生的全部要求（按登记顺序）；学生不存在返回 nil。
func (s *Store) Requirements(student string) []*Requirement {
	if _, ok := s.studentByID[student]; !ok {
		return nil
	}
	var out []*Requirement
	for _, r := range s.requirements {
		if r.StudentID == student {
			out = append(out, r)
		}
	}
	return out
}

// AllRequirements 按登记顺序返回全部要求。
func (s *Store) AllRequirements() []*Requirement {
	return append([]*Requirement(nil), s.requirements...)
}

// Enrollment 返回某学生名下的修读；不存在返回 nil。
func (s *Store) Enrollment(student, id string) *Enrollment {
	return s.enrByKey[ownerKey{student, id}]
}

// Enrollments 返回某学生的全部修读（按登记顺序）；学生不存在返回 nil。
func (s *Store) Enrollments(student string) []*Enrollment {
	if _, ok := s.studentByID[student]; !ok {
		return nil
	}
	var out []*Enrollment
	for _, e := range s.enrollments {
		if e.StudentID == student {
			out = append(out, e)
		}
	}
	return out
}

// Waiver 返回某学生名下的免修申请；不存在返回 nil。
func (s *Store) Waiver(student, id string) *Waiver {
	return s.waiverByKey[ownerKey{student, id}]
}

// Waivers 返回某学生的全部免修历史（按申请顺序）；学生不存在返回 nil。
func (s *Store) Waivers(student string) []*Waiver {
	if _, ok := s.studentByID[student]; !ok {
		return nil
	}
	var out []*Waiver
	for _, w := range s.waivers {
		if w.StudentID == student {
			out = append(out, w)
		}
	}
	return out
}

// ---------- 学生 ----------

// AddStudent 登记学生。编号已存在时原样返回已有学生且 action=existed（幂等）。
func (s *Store) AddStudent(id string) (st *Student, action Action, err error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, "", errors.New("学生编号不能为空")
	}
	if existing := s.studentByID[id]; existing != nil {
		return existing, ActionExisted, nil
	}
	st = &Student{ID: id}
	s.students = append(s.students, st)
	s.studentByID[id] = st
	s.dirty = true
	return st, ActionCreated, nil
}

// ---------- 课程 ----------

// Action 描述登记类操作相对已有记录做了什么。
type Action string

const (
	// ActionCreated 新建了记录。
	ActionCreated Action = "created"
	// ActionExisted 记录已存在且内容一致，原样返回（幂等）。
	ActionExisted Action = "existed"
	// ActionUpdated 记录已存在，本次在允许范围内更新了内容。
	ActionUpdated Action = "updated"
)

// AddCourse 登记课程，学分为正整数，新课程初始开放。
// 编号已存在且名称、学分完全相同时原样返回（幂等），停开/开放状态不变。
// 编号相同但学分不同：该课程尚未被任何课程要求引用时允许修改并保持开放
// 状态；已经被要求引用的课程不能修改学分，请求被拒绝且课程保持不变。
func (s *Store) AddCourse(id, name string, credit int) (c *Course, action Action, err error) {
	id = strings.TrimSpace(id)
	name = strings.TrimSpace(name)
	if id == "" {
		return nil, "", errors.New("课程编号不能为空")
	}
	if name == "" {
		return nil, "", errors.New("课程名称不能为空")
	}
	if credit <= 0 {
		return nil, "", fmt.Errorf("课程学分必须是正整数，得到 %d", credit)
	}
	if existing := s.courseByID[id]; existing != nil {
		if existing.Name == name && existing.Credit == credit {
			return existing, ActionExisted, nil
		}
		if existing.Credit != credit && s.courseReferenced(id) {
			return nil, "", fmt.Errorf(
				"课程 %s 已被课程要求引用，不能修改学分（原学分 %d），原记录保留",
				id, existing.Credit)
		}
		existing.Name = name
		existing.Credit = credit
		s.dirty = true
		return existing, ActionUpdated, nil
	}
	c = &Course{ID: id, Name: name, Credit: credit, Open: true}
	s.courses = append(s.courses, c)
	s.courseByID[id] = c
	s.dirty = true
	return c, ActionCreated, nil
}

// courseReferenced 报告课程是否被任何学生的课程要求引用。
func (s *Store) courseReferenced(id string) bool {
	for _, r := range s.requirements {
		if r.CourseID == id {
			return true
		}
	}
	return false
}

// SetCourseOpen 设置课程开放状态。课程不存在时报错；状态相同则原样返回。
func (s *Store) SetCourseOpen(id string, open bool) (*Course, error) {
	c := s.courseByID[id]
	if c == nil {
		return nil, fmt.Errorf("课程 %s 不存在", id)
	}
	if c.Open != open {
		c.Open = open
		s.dirty = true
	}
	return c, nil
}

// ---------- 课程要求 ----------

// AddRequirement 为学生登记一项指向课程的要求。
// 同一要求编号已存在且指向同一课程时返回原要求（幂等）；指向不同课程则拒绝。
// 同一学生就同一课程已有要求（即使编号不同）也拒绝。学生或课程不存在时拒绝。
func (s *Store) AddRequirement(studentID, reqID, courseID string) (r *Requirement, action Action, err error) {
	studentID = strings.TrimSpace(studentID)
	reqID = strings.TrimSpace(reqID)
	courseID = strings.TrimSpace(courseID)
	if studentID == "" || reqID == "" {
		return nil, "", errors.New("学生编号和要求编号不能为空")
	}
	if _, ok := s.studentByID[studentID]; !ok {
		return nil, "", fmt.Errorf("学生 %s 不存在，不能登记课程要求", studentID)
	}
	c := s.courseByID[courseID]
	if c == nil {
		return nil, "", fmt.Errorf("课程 %s 不存在，不能登记课程要求", courseID)
	}
	if existing := s.reqByKey[ownerKey{studentID, reqID}]; existing != nil {
		if existing.CourseID != courseID {
			return nil, "", fmt.Errorf(
				"学生 %s 的要求编号 %s 已指向课程 %s，不能改指课程 %s",
				studentID, reqID, existing.CourseID, courseID)
		}
		return existing, ActionExisted, nil
	}
	if otherID, ok := s.reqByDup[ownerKey{studentID, courseID}]; ok {
		return nil, "", fmt.Errorf(
			"学生 %s 已就课程 %s 建立要求 %s，不能重复建立", studentID, courseID, otherID)
	}
	r = &Requirement{ID: reqID, StudentID: studentID, CourseID: courseID}
	s.requirements = append(s.requirements, r)
	s.reqByKey[ownerKey{studentID, reqID}] = r
	s.reqByDup[ownerKey{studentID, courseID}] = reqID
	s.dirty = true
	return r, ActionCreated, nil
}

// ---------- 修读 ----------

// AddEnrollment 为学生的一项要求登记某学期的修读，初始状态为“选课”。
// 相同（学生、修读编号、要求、学期）的重复登记返回原记录（幂等）；
// 相同编号但要求或学期不同则拒绝；课程已停开时不能新增修读
// （已登记记录的重复提交仍按幂等返回原记录）。
func (s *Store) AddEnrollment(studentID, reqID, term, enrID string) (e *Enrollment, action Action, err error) {
	studentID = strings.TrimSpace(studentID)
	reqID = strings.TrimSpace(reqID)
	term = strings.TrimSpace(term)
	enrID = strings.TrimSpace(enrID)
	if studentID == "" || reqID == "" || enrID == "" {
		return nil, "", errors.New("学生编号、要求编号和修读编号不能为空")
	}
	if term == "" {
		return nil, "", errors.New("学期不能为空")
	}
	if _, ok := s.studentByID[studentID]; !ok {
		return nil, "", fmt.Errorf("学生 %s 不存在，不能登记修读", studentID)
	}
	r := s.reqByKey[ownerKey{studentID, reqID}]
	if r == nil {
		return nil, "", fmt.Errorf("学生 %s 名下不存在要求 %s，不能登记修读", studentID, reqID)
	}
	if existing := s.enrByKey[ownerKey{studentID, enrID}]; existing != nil {
		if existing.ReqID != reqID || existing.Term != term {
			return nil, "", fmt.Errorf(
				"学生 %s 的修读编号 %s 已用于要求 %s、学期 %s，不能改为要求 %s、学期 %s",
				studentID, enrID, existing.ReqID, existing.Term, reqID, term)
		}
		return existing, ActionExisted, nil
	}
	c := s.courseByID[r.CourseID]
	if c == nil || !c.Open {
		return nil, "", fmt.Errorf("课程 %s 已停开，不能新增修读", r.CourseID)
	}
	e = &Enrollment{
		ID: enrID, StudentID: studentID, ReqID: reqID, Term: term, Result: Enrolled,
	}
	s.enrollments = append(s.enrollments, e)
	s.enrByKey[ownerKey{studentID, enrID}] = e
	s.dirty = true
	return e, ActionCreated, nil
}

// SubmitResult 为修读提交通过/未通过结果。
// 重复提交相同结果返回原记录（幂等，不累加学分）；已提交结果后改提另一结果
// 一律拒绝并保留原记录。
func (s *Store) SubmitResult(studentID, enrID string, result Result) (e *Enrollment, changed bool, err error) {
	studentID = strings.TrimSpace(studentID)
	enrID = strings.TrimSpace(enrID)
	if result != Passed && result != Failed {
		return nil, false, fmt.Errorf("修读结果只能是 %s 或 %s", Passed, Failed)
	}
	if _, ok := s.studentByID[studentID]; !ok {
		return nil, false, fmt.Errorf("学生 %s 不存在", studentID)
	}
	e = s.enrByKey[ownerKey{studentID, enrID}]
	if e == nil {
		return nil, false, fmt.Errorf("学生 %s 名下不存在修读 %s", studentID, enrID)
	}
	if e.Result == result {
		return e, false, nil
	}
	if e.Result != Enrolled {
		return nil, false, fmt.Errorf(
			"修读 %s 已提交结果“%s”，不能改为“%s”，原结果保留", enrID, zhResult(e.Result), zhResult(result))
	}
	s.nextResultSeq++
	e.Result = result
	e.ResultSeq = s.nextResultSeq
	s.dirty = true
	return e, true, nil
}

// ---------- 免修 ----------

// ApplyWaiver 提交免修申请，依据必须非空。
//
// 首次提交且内容合法：状态为有效（approved），action=created。
// 目标要求不存在（含要求属于其他学生）、依据为空、或该要求已有有效免修：
// 申请被拒绝，但申请内容与具体原因保留在该学生的免修历史中，
// 返回的申请状态为 rejected（仍属于新建记录，action=created）。
// 同一编号已存在：内容（要求、依据）完全相同时返回原申请及其结果
// （已拒绝/已撤销的申请不会因重试重新生效）；内容不同则拒绝且不改动原申请。
func (s *Store) ApplyWaiver(studentID, reqID, waiverID, basis string) (w *Waiver, action Action, err error) {
	studentID = strings.TrimSpace(studentID)
	reqID = strings.TrimSpace(reqID)
	waiverID = strings.TrimSpace(waiverID)
	basis = strings.TrimSpace(basis)
	if studentID == "" || reqID == "" || waiverID == "" {
		return nil, "", errors.New("学生编号、要求编号和免修编号不能为空")
	}
	if _, ok := s.studentByID[studentID]; !ok {
		return nil, "", fmt.Errorf("学生 %s 不存在，不能申请免修", studentID)
	}
	if existing := s.waiverByKey[ownerKey{studentID, waiverID}]; existing != nil {
		if existing.ReqID != reqID || existing.Basis != basis {
			return nil, "", fmt.Errorf(
				"学生 %s 的免修编号 %s 已存在（要求 %s、依据 %q），提交内容不同，拒绝修改",
				studentID, waiverID, existing.ReqID, existing.Basis)
		}
		return existing, ActionExisted, nil
	}

	w = &Waiver{ID: waiverID, StudentID: studentID, ReqID: reqID, Basis: basis}
	switch {
	case s.reqByKey[ownerKey{studentID, reqID}] == nil:
		w.Status = WaiverRejected
		w.Reason = fmt.Sprintf("目标要求 %s 不存在或不属于该学生", reqID)
	case basis == "":
		w.Status = WaiverRejected
		w.Reason = "免修依据为空"
	default:
		if other := s.validWaiver(studentID, reqID); other != nil {
			w.Status = WaiverRejected
			w.Reason = fmt.Sprintf("该要求已有有效免修 %s", other.ID)
			break
		}
		w.Status = WaiverApproved
	}
	s.waivers = append(s.waivers, w)
	s.waiverByKey[ownerKey{studentID, waiverID}] = w
	s.dirty = true
	return w, ActionCreated, nil
}

// validWaiver 返回某要求当前有效的免修。
func (s *Store) validWaiver(studentID, reqID string) *Waiver {
	for _, w := range s.waivers {
		if w.StudentID == studentID && w.ReqID == reqID && w.Status == WaiverApproved {
			return w
		}
	}
	return nil
}

// RevokeWaiver 撤销有效免修，原依据与撤销状态保留；reason 可填撤销原因。
// 重复撤销已撤销的免修：原样返回、changed=false，不改变结果。
// 撤销不存在的编号或撤销已被拒绝的申请：明确拒绝。
func (s *Store) RevokeWaiver(studentID, waiverID, reason string) (w *Waiver, changed bool, err error) {
	studentID = strings.TrimSpace(studentID)
	waiverID = strings.TrimSpace(waiverID)
	reason = strings.TrimSpace(reason)
	if _, ok := s.studentByID[studentID]; !ok {
		return nil, false, fmt.Errorf("学生 %s 不存在", studentID)
	}
	w = s.waiverByKey[ownerKey{studentID, waiverID}]
	if w == nil {
		return nil, false, fmt.Errorf("学生 %s 名下不存在免修 %s，不能撤销", studentID, waiverID)
	}
	switch w.Status {
	case WaiverApproved:
		w.Status = WaiverRevoked
		if reason == "" {
			reason = "手动撤销"
		}
		w.Reason = reason
		s.dirty = true
		return w, true, nil
	case WaiverRevoked:
		return w, false, nil
	default:
		return nil, false, fmt.Errorf("免修 %s 已被拒绝，不能撤销（拒绝原因：%s）", waiverID, w.Reason)
	}
}
