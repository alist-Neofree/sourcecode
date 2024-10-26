package task

import "github.com/xhofe/tache"

// GetByCondition get tasks under specific condition
// TODO: replace its call with call to tache.Manager.GetByCondition after Pull Request #4 of github.com/xhofe/tache has merged
func GetByCondition[T TaskInfoWithCreator](m *tache.Manager[T], condition func(T) bool) []T {
	allTasks := m.GetAll()
	var ret []T
	for _, task := range allTasks {
		if condition(task) {
			ret = append(ret, task)
		}
	}
	return ret
}

// RemoveByCondition remove tasks under specific condition
// TODO: replace its call with call to tache.Manager.RemoveByCondition after Pull Request #4 of github.com/xhofe/tache has merged
func RemoveByCondition[T TaskInfoWithCreator](m *tache.Manager[T], condition func(T) bool) {
	tasks := GetByCondition(m, condition)
	for _, task := range tasks {
		m.Remove(task.GetID())
	}
}
