package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pkg/errors"

	"github.com/bytebase/bytebase/api"
	"github.com/bytebase/bytebase/common"
)

// deploymentConfigRaw is the store model for an DeploymentConfig.
// Fields have exactly the same meanings as DeploymentConfig.
type deploymentConfigRaw struct {
	ID int

	// Related fields
	ProjectID int

	// Domain specific fields
	Name    string
	Payload string
}

// toDeploymentConfig creates an instance of DeploymentConfig based on the deploymentConfigRaw.
// This is intended to be called when we need to compose an DeploymentConfig relationship.
func (raw *deploymentConfigRaw) toDeploymentConfig() *api.DeploymentConfig {
	return &api.DeploymentConfig{
		ID: raw.ID,

		// Related fields
		ProjectID: raw.ProjectID,

		// Domain specific fields
		Name:    raw.Name,
		Payload: raw.Payload,
	}
}

// GetDeploymentConfigByProjectID gets an instance of DeploymentConfig.
func (s *Store) GetDeploymentConfigByProjectID(ctx context.Context, projectID int) (api.DeploymentConfig, error) {
	deploymentConfigRaw, err := s.getDeploymentConfigImpl(ctx, &api.DeploymentConfigFind{ProjectID: &projectID})
	if err != nil {
		return api.DeploymentConfig{}, errors.Wrapf(err, "failed to get DeploymentConfig with projectID %d", projectID)
	}
	if deploymentConfigRaw == nil {
		config, err := s.getDefaultDeploymentConfig(ctx, projectID)
		if err != nil {
			return api.DeploymentConfig{}, err
		}
		deploymentConfigRaw = config
	}
	deploymentConfig, err := s.composeDeploymentConfig(ctx, deploymentConfigRaw)
	if err != nil {
		return api.DeploymentConfig{}, errors.Wrapf(err, "failed to compose DeploymentConfig with deploymentConfigRaw[%+v]", deploymentConfigRaw)
	}
	return *deploymentConfig, nil
}

func (s *Store) getDefaultDeploymentConfig(ctx context.Context, projectID int) (*deploymentConfigRaw, error) {
	environmentList, err := s.ListEnvironmentV2(ctx, &FindEnvironmentMessage{})
	if err != nil {
		return nil, err
	}
	scheduleList := api.DeploymentSchedule{}
	for _, environment := range environmentList {
		scheduleList.Deployments = append(scheduleList.Deployments, &api.Deployment{
			Name: fmt.Sprintf("%s Stage", environment.Title),
			Spec: &api.DeploymentSpec{
				Selector: &api.LabelSelector{
					MatchExpressions: []*api.LabelSelectorRequirement{
						{Key: "bb.environment", Operator: api.InOperatorType, Values: []string{environment.ResourceID}},
					},
				},
			},
		})
	}
	bytes, err := json.Marshal(scheduleList)
	if err != nil {
		return nil, err
	}
	return &deploymentConfigRaw{
		ID:        0,
		ProjectID: projectID,
		Payload:   string(bytes),
	}, nil
}

// UpsertDeploymentConfig upserts an instance of DeploymentConfig.
func (s *Store) UpsertDeploymentConfig(ctx context.Context, upsert *api.DeploymentConfigUpsert) (*api.DeploymentConfig, error) {
	deploymentConfigRaw, err := s.upsertDeploymentConfigRaw(ctx, upsert)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to upsert deployment config with DeploymentConfigUpsert[%+v]", upsert)
	}
	deploymentConfig, err := s.composeDeploymentConfig(ctx, deploymentConfigRaw)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to compose DeploymentConfig with deploymentConfigRaw[%+v]", deploymentConfigRaw)
	}
	return deploymentConfig, nil
}

//
// private functions
//

func (s *Store) composeDeploymentConfig(ctx context.Context, raw *deploymentConfigRaw) (*api.DeploymentConfig, error) {
	deploymentConfig := raw.toDeploymentConfig()
	project, err := s.GetProjectByID(ctx, deploymentConfig.ProjectID)
	if err != nil {
		return nil, err
	}
	deploymentConfig.Project = project
	return deploymentConfig, nil
}

// upsertDeploymentConfigRaw upserts a deployment configuration to a project.
func (s *Store) upsertDeploymentConfigRaw(ctx context.Context, upsert *api.DeploymentConfigUpsert) (*deploymentConfigRaw, error) {
	// Validate the deployment configuration.
	if _, err := api.ValidateAndGetDeploymentSchedule(upsert.Payload); err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, FormatError(err)
	}
	defer tx.Rollback()

	cfg, err := s.upsertDeploymentConfigImpl(ctx, tx, upsert)
	if err != nil {
		return nil, FormatError(err)
	}

	if err := tx.Commit(); err != nil {
		return nil, FormatError(err)
	}

	return cfg, nil
}

// getDeploymentConfigImpl finds the deployment configuration in a project.
func (s *Store) getDeploymentConfigImpl(ctx context.Context, find *api.DeploymentConfigFind) (*deploymentConfigRaw, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, FormatError(err)
	}
	defer tx.Rollback()

	// Build WHERE clause.
	where, args := []string{"1 = 1"}, []interface{}{}
	if v := find.ID; v != nil {
		where, args = append(where, fmt.Sprintf("id = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.ProjectID; v != nil {
		where, args = append(where, fmt.Sprintf("project_id = $%d", len(args)+1)), append(args, *v)
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT
			id,
			project_id,
			name,
			config
		FROM deployment_config
		WHERE `+strings.Join(where, " AND "),
		args...,
	)
	if err != nil {
		return nil, FormatError(err)
	}
	defer rows.Close()

	// Iterate over result set and deserialize rows into list.
	var ret []*deploymentConfigRaw
	for rows.Next() {
		var cfg deploymentConfigRaw
		if err := rows.Scan(
			&cfg.ID,
			&cfg.ProjectID,
			&cfg.Name,
			&cfg.Payload,
		); err != nil {
			return nil, FormatError(err)
		}

		ret = append(ret, &cfg)
	}
	if err := rows.Err(); err != nil {
		return nil, FormatError(err)
	}

	switch len(ret) {
	case 0:
		return nil, nil
	case 1:
		return ret[0], nil
	default:
		return nil, &common.Error{Code: common.Conflict, Err: errors.Errorf("found %d deployment configurations with filter %+v, expect 1", len(ret), find)}
	}
}

func (*Store) upsertDeploymentConfigImpl(ctx context.Context, tx *Tx, upsert *api.DeploymentConfigUpsert) (*deploymentConfigRaw, error) {
	if upsert.Payload == "" {
		upsert.Payload = "{}"
	}
	query := `
		INSERT INTO deployment_config (
			creator_id,
			updater_id,
			project_id,
			name,
			config
		)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT(project_id) DO UPDATE SET
			updater_id = excluded.updater_id,
			name = excluded.name,
			config = excluded.config
		RETURNING id, project_id, name, config
	`
	var cfg deploymentConfigRaw
	if err := tx.QueryRowContext(ctx, query,
		upsert.UpdaterID,
		upsert.UpdaterID,
		upsert.ProjectID,
		upsert.Name,
		upsert.Payload,
	).Scan(
		&cfg.ID,
		&cfg.ProjectID,
		&cfg.Name,
		&cfg.Payload,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, common.FormatDBErrorEmptyRowWithQuery(query)
		}
		return nil, err
	}
	return &cfg, nil
}
