package service

import (
	"context"
	"sync"
)

type Service interface {
	Run(serviceStopCtx context.Context, serviceStopWaiter *sync.WaitGroup)
}
