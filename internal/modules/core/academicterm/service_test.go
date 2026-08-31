package academicterm

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestServiceCreatesOneOpenDefaultAndClosesWithVersion(t *testing.T) {
	service, err := NewService(NewMemoryRepository())
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC) }
	ctx := context.Background()
	first, err := service.Create(ctx, "100", CreateRequest{
		Year: 2026, Semester: SemesterFall, FirstWeekStart: "2026-08-31", Status: StatusOpen, IsDefault: true, Reason: "创建秋季学期",
	})
	if err != nil {
		t.Fatalf("create first term: %v", err)
	}
	second, err := service.Create(ctx, "100", CreateRequest{
		Year: 2027, Semester: SemesterSpring, FirstWeekStart: "2027-03-01", Status: StatusOpen, IsDefault: true, Reason: "创建春季学期",
	})
	if err != nil {
		t.Fatalf("create second term: %v", err)
	}
	defaultTerm, err := service.DefaultOpen(ctx)
	if err != nil || defaultTerm.ID != second.ID {
		t.Fatalf("default term = %#v, %v", defaultTerm, err)
	}
	first, err = service.repository.Get(ctx, first.ID)
	if err != nil {
		t.Fatalf("reload first term after default handoff: %v", err)
	}
	if _, err := service.Close(ctx, "100", first.ID, TransitionRequest{ExpectedVersion: first.Version, Reason: "学期结束关闭"}); err != nil {
		t.Fatalf("close first term: %v", err)
	}
	if _, err := service.GetAvailable(ctx, first.ID); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed term availability error = %v", err)
	}
	if _, err := service.UpdateFirstWeek(ctx, "100", first.ID, UpdateRequest{ExpectedVersion: first.Version, FirstWeekStart: "2026-09-07", Reason: "错误更新"}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale update error = %v", err)
	}
}

func TestServiceRejectsNonMondayAndClosedDefault(t *testing.T) {
	service, err := NewService(NewMemoryRepository())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := service.Create(ctx, "100", CreateRequest{
		Year: 2026, Semester: SemesterFall, FirstWeekStart: "2026-09-01", Status: StatusOpen, Reason: "错误日期",
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("non-Monday error = %v", err)
	}
	if _, err := service.Create(ctx, "100", CreateRequest{
		Year: 2026, Semester: SemesterFall, FirstWeekStart: "2026-08-31", Status: StatusClosed, IsDefault: true, Reason: "错误默认",
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("closed default error = %v", err)
	}
}
