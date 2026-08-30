package apperror

import (
	"errors"
	"testing"
)

func TestCatalogIsValidAndMachineCodesAreUnique(t *testing.T) {
	if err := ValidateCatalog(Catalog()); err != nil {
		t.Fatal(err)
	}
}

func TestTranslatorMapsKnownAndSanitizesUnknownErrors(t *testing.T) {
	known := errors.New("known domain failure")
	translator := MustTranslator("test", InternalError, Rule{Target: known, Descriptor: RequestInvalid})

	mapped := translator.Translate(known)
	if mapped.Descriptor() != RequestInvalid || !errors.Is(mapped, known) {
		t.Fatalf("known translation = %#v", mapped)
	}

	secret := errors.New("database password=secret")
	unknown := translator.Translate(secret)
	if unknown.Descriptor() != InternalError || unknown.Error() != "系统暂时无法完成该操作，请稍后重试；如持续出现，请联系管理员。" || !errors.Is(unknown, secret) {
		t.Fatalf("unknown translation = %#v", unknown)
	}
}

func TestTranslatorRejectsUnregisteredDescriptor(t *testing.T) {
	_, err := NewTranslator("test", Descriptor{
		Owner: "test", MachineCode: "test.unregistered", LegacyCode: 90001,
		HTTPStatus: 400, Message: "test error",
	})
	if err == nil {
		t.Fatal("expected unregistered descriptor rejection")
	}
}
