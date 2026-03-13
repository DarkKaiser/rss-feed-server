package service

import (
	"context"
	"sync"
)

type Service interface {
	Start(serviceStopCtx context.Context, serviceStopWG *sync.WaitGroup) error
}
