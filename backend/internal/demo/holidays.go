package demo

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/availability"
	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/db"
)

// holidayLeadDays puts the company's own day off a little ahead, where the plan opens.
const holidayLeadDays = 12

// Holidays fills the organization's default calendar with the year's usual
// days off and one of the company's own. A calendar with days already is left alone.
func Holidays(ctx context.Context, cluster *db.Cluster, actor uuid.UUID, today time.Time) error {
	calendars := availability.NewService(cluster)
	found, err := calendars.ListCalendars(ctx)
	if err != nil {
		return fmt.Errorf("list holiday calendars: %w", err)
	}
	var calendar *availability.HolidayCalendar
	for i := range found {
		if found[i].Default {
			calendar = &found[i]
		}
	}
	if calendar == nil {
		name := bootstrap.DefaultHolidayCalendarName
		if calendar, _, err = calendars.CreateCalendar(ctx, availability.CalendarInput{Name: &name}, actor); err != nil {
			return fmt.Errorf("make the holiday calendar: %w", err)
		}
	}
	if calendar.DayCount > 0 {
		return nil
	}

	var days []availability.Holiday
	for _, year := range []int{today.Year(), today.Year() + 1} {
		on := func(month time.Month, day int) string {
			return time.Date(year, month, day, 0, 0, 0, 0, time.UTC).Format(availability.DateLayout)
		}
		days = append(days,
			availability.Holiday{Day: on(time.January, 1), Name: "New Year's Day"},
			availability.Holiday{Day: on(time.May, 1), Name: "Labour Day"},
			availability.Holiday{Day: on(time.December, 24), Name: "Christmas Eve", HalfDay: true},
			availability.Holiday{Day: on(time.December, 25), Name: "Christmas Day"},
			availability.Holiday{Day: on(time.December, 26), Name: "Boxing Day"},
		)
	}
	own := today.AddDate(0, 0, holidayLeadDays).Format(availability.DateLayout)
	if _, taken := availability.DaysOf(days)[own]; !taken {
		days = append(days, availability.Holiday{Day: own, Name: "Company day off"})
	}
	if _, _, err := calendars.ReplaceDays(ctx, calendar.ID, days, actor); err != nil {
		return fmt.Errorf("fill the holiday calendar: %w", err)
	}
	return nil
}
