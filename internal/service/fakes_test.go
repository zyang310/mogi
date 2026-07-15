package service

import (
	"context"

	"mogi/internal/access"
	"mogi/internal/ai"
	"mogi/internal/hotkey"
	"mogi/internal/models"
)

// Function-field fakes shared by every service test. Each test overrides only
// the calls it asserts on; un-overridden reads return zero values and
// un-overridden writes succeed silently. None of them touch SQLite, the
// network, or OS hooks — that isolation is the point of the service layer.

// fakeStore satisfies all four per-service store interfaces (InterviewStore,
// HistoryStore, VoiceStore, SettingsStore) so one fake serves every test.
type fakeStore struct {
	getPreferences        func() (models.Preferences, error)
	savePreferences       func(p models.Preferences) error
	getAPIKey             func(provider string) (string, error)
	setAPIKey             func(provider, value string) error
	deleteAPIKey          func(provider string) error
	createSession         func(id, problemID, model string) (models.Session, error)
	endSession            func(id string) error
	addMessage            func(msg models.Message) error
	getMessages           func(sessionID string) ([]models.Message, error)
	getSession            func(id string) (models.Session, error)
	updateSessionMeta     func(id, title, difficulty, finalCode string) error
	setSessionCompany     func(id, company, mode string) error
	listSessions          func() ([]models.SessionSummary, error)
	deleteSession         func(id string) error
	getSessionFinalCode   func(id string) (string, error)
	getSessionDebrief     func(id string) (string, error)
	saveSessionDebrief    func(id, debrief string) error
	listStarredCompanies  func() ([]string, error)
	setCompanyStarred     func(slug string, starred bool) error
	clearAll              func() error
	getManagedKey         func(provider string) (string, error)
	setManagedKey         func(provider, value string) error
	getManagedSession     func() (string, error)
	setManagedSession     func(token string) error
	getManagedEmail       func() (string, error)
	setManagedEmail       func(email string) error
	getManagedPinnedModel func() (string, error)
	setManagedPinnedModel func(model string) error
	deleteManagedData     func() error
}

func (f *fakeStore) GetPreferences() (models.Preferences, error) {
	if f.getPreferences != nil {
		return f.getPreferences()
	}
	return models.Preferences{}, nil
}

func (f *fakeStore) SavePreferences(p models.Preferences) error {
	if f.savePreferences != nil {
		return f.savePreferences(p)
	}
	return nil
}

func (f *fakeStore) GetAPIKey(provider string) (string, error) {
	if f.getAPIKey != nil {
		return f.getAPIKey(provider)
	}
	return "", nil
}

func (f *fakeStore) SetAPIKey(provider, value string) error {
	if f.setAPIKey != nil {
		return f.setAPIKey(provider, value)
	}
	return nil
}

func (f *fakeStore) DeleteAPIKey(provider string) error {
	if f.deleteAPIKey != nil {
		return f.deleteAPIKey(provider)
	}
	return nil
}

func (f *fakeStore) ListStarredCompanies() ([]string, error) {
	if f.listStarredCompanies != nil {
		return f.listStarredCompanies()
	}
	return nil, nil
}

func (f *fakeStore) SetCompanyStarred(slug string, starred bool) error {
	if f.setCompanyStarred != nil {
		return f.setCompanyStarred(slug, starred)
	}
	return nil
}

func (f *fakeStore) ClearAll() error {
	if f.clearAll != nil {
		return f.clearAll()
	}
	return nil
}

func (f *fakeStore) GetManagedKey(provider string) (string, error) {
	if f.getManagedKey != nil {
		return f.getManagedKey(provider)
	}
	return "", nil
}

func (f *fakeStore) SetManagedKey(provider, value string) error {
	if f.setManagedKey != nil {
		return f.setManagedKey(provider, value)
	}
	return nil
}

func (f *fakeStore) GetManagedSession() (string, error) {
	if f.getManagedSession != nil {
		return f.getManagedSession()
	}
	return "", nil
}

func (f *fakeStore) SetManagedSession(token string) error {
	if f.setManagedSession != nil {
		return f.setManagedSession(token)
	}
	return nil
}

func (f *fakeStore) GetManagedEmail() (string, error) {
	if f.getManagedEmail != nil {
		return f.getManagedEmail()
	}
	return "", nil
}

func (f *fakeStore) SetManagedEmail(email string) error {
	if f.setManagedEmail != nil {
		return f.setManagedEmail(email)
	}
	return nil
}

func (f *fakeStore) GetManagedPinnedModel() (string, error) {
	if f.getManagedPinnedModel != nil {
		return f.getManagedPinnedModel()
	}
	return "", nil
}

func (f *fakeStore) SetManagedPinnedModel(model string) error {
	if f.setManagedPinnedModel != nil {
		return f.setManagedPinnedModel(model)
	}
	return nil
}

func (f *fakeStore) DeleteManagedData() error {
	if f.deleteManagedData != nil {
		return f.deleteManagedData()
	}
	return nil
}

func (f *fakeStore) CreateSession(id, problemID, model string) (models.Session, error) {
	if f.createSession != nil {
		return f.createSession(id, problemID, model)
	}
	return models.Session{ID: id, ProblemID: problemID, Model: model}, nil
}

func (f *fakeStore) EndSession(id string) error {
	if f.endSession != nil {
		return f.endSession(id)
	}
	return nil
}

func (f *fakeStore) AddMessage(msg models.Message) error {
	if f.addMessage != nil {
		return f.addMessage(msg)
	}
	return nil
}

func (f *fakeStore) GetMessages(sessionID string) ([]models.Message, error) {
	if f.getMessages != nil {
		return f.getMessages(sessionID)
	}
	return nil, nil
}

func (f *fakeStore) GetSession(id string) (models.Session, error) {
	if f.getSession != nil {
		return f.getSession(id)
	}
	return models.Session{ID: id}, nil
}

func (f *fakeStore) UpdateSessionMeta(id, title, difficulty, finalCode string) error {
	if f.updateSessionMeta != nil {
		return f.updateSessionMeta(id, title, difficulty, finalCode)
	}
	return nil
}

func (f *fakeStore) SetSessionCompany(id, company, mode string) error {
	if f.setSessionCompany != nil {
		return f.setSessionCompany(id, company, mode)
	}
	return nil
}

func (f *fakeStore) ListSessions() ([]models.SessionSummary, error) {
	if f.listSessions != nil {
		return f.listSessions()
	}
	return nil, nil
}

func (f *fakeStore) DeleteSession(id string) error {
	if f.deleteSession != nil {
		return f.deleteSession(id)
	}
	return nil
}

func (f *fakeStore) GetSessionFinalCode(id string) (string, error) {
	if f.getSessionFinalCode != nil {
		return f.getSessionFinalCode(id)
	}
	return "", nil
}

func (f *fakeStore) GetSessionDebrief(id string) (string, error) {
	if f.getSessionDebrief != nil {
		return f.getSessionDebrief(id)
	}
	return "", nil
}

func (f *fakeStore) SaveSessionDebrief(id, debrief string) error {
	if f.saveSessionDebrief != nil {
		return f.saveSessionDebrief(id, debrief)
	}
	return nil
}

// fakeAI satisfies the AI interface without spending tokens.
type fakeAI struct {
	complete        func(model string, msgs []ai.ChatMessage) (string, error)
	extractMeta     func(model, transcript, screenshot string) (ai.SessionMeta, error)
	generateDebrief func(model, transcript, finalCode string) (models.Debrief, error)
	listModels      func() ([]models.Model, error)
}

func (f *fakeAI) Complete(_ context.Context, model string, msgs []ai.ChatMessage) (string, error) {
	if f.complete != nil {
		return f.complete(model, msgs)
	}
	return "", nil
}

func (f *fakeAI) ExtractSessionMeta(_ context.Context, model, transcript, screenshotB64 string) (ai.SessionMeta, error) {
	if f.extractMeta != nil {
		return f.extractMeta(model, transcript, screenshotB64)
	}
	return ai.SessionMeta{}, nil
}

func (f *fakeAI) GenerateDebrief(_ context.Context, model, transcript, finalCode string) (models.Debrief, error) {
	if f.generateDebrief != nil {
		return f.generateDebrief(model, transcript, finalCode)
	}
	return models.Debrief{}, nil
}

func (f *fakeAI) ListModels(_ context.Context) ([]models.Model, error) {
	if f.listModels != nil {
		return f.listModels()
	}
	return nil, nil
}

// fakeAccess satisfies AccessClient so account tests never make HTTP calls.
// Un-overridden calls succeed with zero values.
type fakeAccess struct {
	requestCode func(email, inviteCode string) error
	verify      func(email, code string) (string, access.KeySet, error)
	keys        func(token string) (access.KeySet, error)
}

func (f *fakeAccess) RequestCode(_ context.Context, email, inviteCode string) error {
	if f.requestCode != nil {
		return f.requestCode(email, inviteCode)
	}
	return nil
}

func (f *fakeAccess) Verify(_ context.Context, email, code string) (string, access.KeySet, error) {
	if f.verify != nil {
		return f.verify(email, code)
	}
	return "", access.KeySet{}, nil
}

func (f *fakeAccess) Keys(_ context.Context, token string) (access.KeySet, error) {
	if f.keys != nil {
		return f.keys(token)
	}
	return access.KeySet{}, nil
}

// fakeSpeech satisfies Speech (TTS + STT) for provider-resolution tests.
type fakeSpeech struct {
	synthesize func(voiceID, text string) ([]byte, error)
	listVoices func() ([]models.Voice, error)
	transcribe func(audio []byte, mimeType string) (string, error)
}

func (f *fakeSpeech) Synthesize(_ context.Context, voiceID, text string) ([]byte, error) {
	if f.synthesize != nil {
		return f.synthesize(voiceID, text)
	}
	return nil, nil
}

func (f *fakeSpeech) ListVoices(_ context.Context) ([]models.Voice, error) {
	if f.listVoices != nil {
		return f.listVoices()
	}
	return nil, nil
}

func (f *fakeSpeech) Transcribe(_ context.Context, audio []byte, mimeType string) (string, error) {
	if f.transcribe != nil {
		return f.transcribe(audio, mimeType)
	}
	return "", nil
}

// fakeScreen records capture-control calls so interview/settings tests can
// assert region propagation and start/stop without touching the real screen.
type fakeScreen struct {
	latest  string
	regions [][5]float64 // {displayIndex, x, y, w, h} per SetRegion call
	starts  []int        // intervalMs per Start call
	stops   int
}

func (f *fakeScreen) SetRegion(displayIndex int, x, y, w, h float64) {
	f.regions = append(f.regions, [5]float64{float64(displayIndex), x, y, w, h})
}

func (f *fakeScreen) Start(_ context.Context, intervalMs int) {
	f.starts = append(f.starts, intervalMs)
}

func (f *fakeScreen) Stop() { f.stops++ }

func (f *fakeScreen) Latest() string { return f.latest }

// fakeHotkey records Apply calls so settings tests never install the real OS
// keyboard hook (libuiohook segfaults readily outside a real session).
type fakeHotkey struct {
	applies []struct {
		enabled bool
		spec    hotkey.Spec
	}
}

func (f *fakeHotkey) Apply(_ context.Context, enabled bool, spec hotkey.Spec) {
	f.applies = append(f.applies, struct {
		enabled bool
		spec    hotkey.Spec
	}{enabled, spec})
}
