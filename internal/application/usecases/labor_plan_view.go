package usecases

import (
	"context"

	"github.com/claudioed/wes-work-planning/internal/application/ports"
	"github.com/claudioed/wes-work-planning/internal/domain/laborview"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
)

// LaborPlanView is a read-only query use case exposing the projected
// LaborPlanObserved read model for a path.
type LaborPlanView struct {
	views ports.LaborPlanViewRepo
}

func NewLaborPlanView(views ports.LaborPlanViewRepo) *LaborPlanView {
	return &LaborPlanView{views: views}
}

type LaborPlanViewRequest struct {
	PathId shared.PathId
}

func (uc *LaborPlanView) Execute(ctx context.Context, req LaborPlanViewRequest) (laborview.LaborPlanObserved, error) {
	return uc.views.FindByPathId(ctx, req.PathId)
}
