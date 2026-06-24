package cronjob

import (
	"github.com/by-sonic/HyPanel/database"
	"github.com/by-sonic/HyPanel/logger"
	"github.com/by-sonic/HyPanel/service"
)

type DepleteJob struct {
	service.ClientService
	service.InboundService
}

func NewDepleteJob() *DepleteJob {
	return new(DepleteJob)
}

func (s *DepleteJob) Run() {
	inboundIds, err := s.ClientService.DepleteClients()
	if err != nil {
		logger.Warning("Disable depleted users failed: ", err)
		return
	}
	if len(inboundIds) > 0 {
		err := s.InboundService.ApplyUserChanges(database.GetDB(), inboundIds)
		if err != nil {
			logger.Error("unable to apply inbound user changes: ", err)
		}
	}
}
