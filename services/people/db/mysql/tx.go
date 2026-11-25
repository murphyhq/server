package mysql

import (
	"context"
	"time"

	"github.com/murphyhq/server"
	murphymysql "github.com/murphyhq/server/internal/mysql"
	"github.com/murphyhq/server/internal/tracer"
)

type Tx struct {
	tx      murphymysql.DBTX
	queries *Queries
}

func (t *Tx) CreateUser(ctx context.Context, user *murphy.User) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return murphymysql.Error("user", t.queries.CreateUser(ctx, CreateUserParams{
		ID: user.GetId(), Name: user.GetName(), Email: user.GetEmail(), TimeZone: user.GetTimeZone(),
		Status: user.GetStatus().Text(), Role: user.GetRole().Text(), CreatedAt: user.GetCreatedAt().AsTime(), UpdatedAt: user.GetUpdatedAt().AsTime(),
	}))
}

func (t *Tx) CreateIdentity(ctx context.Context, identity *murphy.Identity) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return insertIdentity(ctx, t.queries, identity)
}

func (t *Tx) AdminExists(ctx context.Context) (_ bool, err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return t.queries.AdminExists(ctx)
}

func (t *Tx) SetUserRole(ctx context.Context, id string, role murphy.UserRole, at time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows, err := t.queries.SetUserRole(ctx, SetUserRoleParams{ID: id, Role: role.Text(), UpdatedAt: at})
	return murphymysql.Changed("user", rows, err)
}

func (t *Tx) RevokeSessionsOfUser(ctx context.Context, userID string, at time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	_, err = t.queries.RevokeSessionsOfUser(ctx, RevokeSessionsOfUserParams{RevokedAt: &at, UpdatedAt: at, UserID: userID})
	return murphymysql.Error("session", err)
}

func (t *Tx) DiscardOneTimeCodesOfUser(ctx context.Context, userID string, at time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	_, err = t.queries.DiscardOneTimeCodesOfUser(ctx, DiscardOneTimeCodesOfUserParams{UsedAt: &at, UpdatedAt: at, UserID: userID})
	return murphymysql.Error("one-time code", err)
}

func (t *Tx) CreateContactMethod(ctx context.Context, method *murphy.ContactMethod) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return insertContactMethod(ctx, t.queries, method)
}

func (t *Tx) DeleteNotificationRule(ctx context.Context, userID string, urgency murphy.Urgency) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	return deleteNotificationRule(ctx, t.queries, userID, urgency)
}

func (t *Tx) CreateNotificationRule(ctx context.Context, rule *murphy.NotificationRule, at time.Time) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	rows := murphymysql.Rows{Name: "CreateNotificationRules", Table: "notification_rules", Columns: []string{"id", "user_id", "urgency", "position", "contact_method_id", "delay_seconds", "created_at"}}
	for i, step := range rule.GetSteps() {
		rows.Values = append(rows.Values, []any{murphy.NewID(), rule.GetUserId(), rule.GetUrgency().Text(), i + 1, step.GetContactMethodId(), int32(step.GetDelay().AsDuration() / time.Second), at})
	}
	return murphymysql.Error("notification rule", murphymysql.Insert(ctx, t.tx, rows))
}
