package model

type ModelType string

type Finder interface {
	Find(modelType ModelType) interface{}
}
