package auth

const (
	NO_PERMISSIONS uint = 0

	CREATE_PREFERENCE uint = 1 << iota
	UPDATE_PREFERENCE
	DELETE_PREFERENCE

	CREATE_BLOQ
	UPDATE_BLOQ
	DELETE_BLOQ

    PREFERENCE_MANAGER = CREATE_PREFERENCE | UPDATE_PREFERENCE | DELETE_PREFERENCE
)
