package common

import (
	"github.com/go-playground/validator/v10"
)

var validate = validator.New()

// ValidateStruct 验证结构体
func ValidateStruct(s interface{}) error {
	return validate.Struct(s)
}
