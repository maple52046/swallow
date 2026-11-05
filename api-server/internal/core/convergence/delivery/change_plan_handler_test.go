package delivery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"

	"github.com/maple52046/swallow/internal/core/convergence"
	"github.com/maple52046/swallow/internal/core/convergence/convergencetest"
	"github.com/maple52046/swallow/internal/core/store/storetest"
	"github.com/maple52046/swallow/internal/shared/apicontract"
	"github.com/maple52046/swallow/internal/shared/jwt"
	"github.com/maple52046/swallow/internal/shared/middleware"
)

// plans is a ChangePlanRepository over a map, enough for the handler.
type plans map[string]*convergence.ChangePlan

func (p plans) Create(_ context.Context, plan *convergence.ChangePlan) error {
	p[plan.ID] = plan
	return nil
}

func (p plans) FindByID(_ context.Context, id string) (*convergence.ChangePlan, error) {
	if plan, ok := p[id]; ok {
		clone := *plan
		return &clone, nil
	}
	return nil, convergence.ErrChangePlanNotFound
}

func (p plans) FindUnfinished(_ context.Context, resource convergence.ResourceRef) (*convergence.ChangePlan, error) {
	for _, plan := range p {
		if plan.Resource == resource && plan.State.Unfinished() {
			return plan, nil
		}
	}
	return nil, convergence.ErrChangePlanNotFound
}

func (p plans) List(context.Context, convergence.ChangePlanFilter) ([]*convergence.ChangePlan, error) {
	out := make([]*convergence.ChangePlan, 0, len(p))
	for _, plan := range p {
		out = append(out, plan)
	}
	return out, nil
}

func (p plans) ListExpired(context.Context, time.Time) ([]*convergence.ChangePlan, error) {
	return nil, nil
}

func (p plans) Transition(_ context.Context, id string, from []convergence.ChangePlanState, to convergence.ChangePlanState, transition convergence.PlanTransition) error {
	plan, ok := p[id]
	if !ok {
		return convergence.ErrChangePlanNotFound
	}
	if !slices.Contains(from, plan.State) {
		return convergence.ErrChangePlanState
	}
	plan.State, plan.StateReason = to, transition.Reason
	if transition.DecidedBy != "" {
		plan.DecidedBy, plan.DecidedAt = transition.DecidedBy, transition.DecidedAt
	}
	return nil
}

func newTestService(t *testing.T, repo plans) *convergence.ChangePlanService {
	t.Helper()
	service, err := convergence.NewChangePlanService(convergence.ChangePlanServiceDeps{
		Transactor: &storetest.Transactor{}, Plans: repo, Policies: &convergencetest.Policies{}, TTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewChangePlanService() error = %v", err)
	}
	return service
}

// The responses follow the contract: the list and plan shapes, an approval, and the 404 and 409
// error envelopes.
func TestChangePlanRoutesFollowTheContract(t *testing.T) {
	repo := plans{}
	service := newTestService(t, repo)
	if _, err := service.Propose(context.Background(), convergence.ResourceRef{Kind: "platform", ID: "p1"}, convergence.PlanDraft{
		Generation: 2, ObservedAt: time.Now(),
		Actions: []convergence.PlannedAction{{Target: convergence.ResourceRef{Kind: "server", ID: "s1"}, Action: "remove-member", From: "worker", Destructive: true, Reason: "removed from the spec"}},
		Effects: []string{"s1 leaves the Platform"},
	}, ""); err != nil {
		t.Fatal(err)
	}
	var planID string
	for id := range repo {
		planID = id
	}

	app := fiber.New()
	app.Use(requestid.New())
	app.Use(apicontract.ResponseValidator(t))
	NewChangePlanHandler(service).RegisterRoutes(app.Group("/api/v1/change-plans"))

	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, "/api/v1/change-plans?state=pending_approval", http.StatusOK},
		{http.MethodGet, "/api/v1/change-plans?state=bogus", http.StatusBadRequest},
		{http.MethodGet, "/api/v1/change-plans/" + planID, http.StatusOK},
		{http.MethodPost, "/api/v1/change-plans/" + planID + "/approve", http.StatusOK},
		{http.MethodPost, "/api/v1/change-plans/" + planID + "/approve", http.StatusConflict},
		{http.MethodGet, "/api/v1/change-plans/missing", http.StatusNotFound},
	} {
		resp, err := app.Test(httptest.NewRequest(tc.method, tc.path, nil))
		if err != nil {
			t.Fatalf("%s %s: %v", tc.method, tc.path, err)
		}
		if resp.StatusCode != tc.status {
			t.Errorf("%s %s = %d, want %d", tc.method, tc.path, resp.StatusCode, tc.status)
		}
		if tc.status == http.StatusOK && tc.method == http.MethodPost {
			var body ChangePlanResponse
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body.State != "approved" {
				t.Errorf("approve body = %+v, %v", body, err)
			}
		}
		_ = resp.Body.Close()
	}
}

// The policy routes follow the contract: a policy never stored reads as approve on submit with a
// null updatedAt, a replacement records the admin and is read back, and a body without a boolean
// approveOnSubmit is refused without changing the policy.
func TestChangePlanPolicyRoutesFollowTheContract(t *testing.T) {
	service := newTestService(t, plans{})
	jwtSvc := jwt.NewService("test-secret", time.Hour)
	token, err := jwtSvc.Sign("u1", "admin", "admin")
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	app := fiber.New()
	app.Use(requestid.New())
	app.Use(apicontract.ResponseValidator(t))
	NewChangePlanHandler(service).RegisterPolicyRoutes(app.Group("/api/v1/change-plan-policy", middleware.Auth(jwtSvc), middleware.AdminOnly()))

	call := func(method, body string) (int, ChangePlanPolicyResponse) {
		t.Helper()
		request := httptest.NewRequest(method, "/api/v1/change-plan-policy", strings.NewReader(body))
		request.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
		request.Header.Set(fiber.HeaderAuthorization, "Bearer "+token)
		resp, err := app.Test(request)
		if err != nil {
			t.Fatalf("%s /change-plan-policy: %v", method, err)
		}
		defer resp.Body.Close()
		var policy ChangePlanPolicyResponse
		if resp.StatusCode == http.StatusOK {
			if err := json.NewDecoder(resp.Body).Decode(&policy); err != nil {
				t.Fatalf("%s /change-plan-policy body: %v", method, err)
			}
		}
		return resp.StatusCode, policy
	}

	if status, policy := call(http.MethodGet, ""); status != http.StatusOK || !policy.ApproveOnSubmit || policy.UpdatedAt != nil || policy.UpdatedBy != "" {
		t.Errorf("GET never-stored policy = %d %+v, want approve on submit, never updated", status, policy)
	}
	if status, policy := call(http.MethodPut, `{"approveOnSubmit": false}`); status != http.StatusOK || policy.ApproveOnSubmit ||
		policy.UpdatedAt == nil || policy.UpdatedBy != "admin" {
		t.Errorf("PUT false = %d %+v, want the switch on, updated by admin", status, policy)
	}
	for _, body := range []string{`{}`, `{"approveOnSubmit": "no"}`, `not json`} {
		if status, _ := call(http.MethodPut, body); status != http.StatusBadRequest {
			t.Errorf("PUT %s = %d, want 400", body, status)
		}
	}
	if status, policy := call(http.MethodGet, ""); status != http.StatusOK || policy.ApproveOnSubmit {
		t.Errorf("GET after the refused writes = %d %+v, want the switch still on", status, policy)
	}
}
