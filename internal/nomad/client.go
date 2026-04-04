// Package nomad handles Nomad API interaction: discovering jobs, task groups,
// tasks, and their current declared resource specs.
package nomad

import (
	"context"
	"fmt"

	nomadapi "github.com/hashicorp/nomad/api"
)

// TaskSpec holds the identifying information and current resource spec for
// a single Nomad task. This is the unit that NRR generates recommendations for.
type TaskSpec struct {
	Namespace string
	Job       string
	Group     string
	Task      string

	// Current declared resources (what the job file says).
	CPUMHz   int // CPU in MHz as declared in the job spec
	MemoryMB int // Memory in MB as declared in the job spec
}

// Client wraps the Nomad API client.
type Client struct {
	api *nomadapi.Client
}

// NewClient creates a new Nomad API client pointing at the given address.
func NewClient(address string) (*Client, error) {
	cfg := nomadapi.DefaultConfig()
	cfg.Address = address

	client, err := nomadapi.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating Nomad client: %w", err)
	}
	return &Client{api: client}, nil
}

// DiscoverTasks lists all running tasks across all jobs in the given namespace,
// optionally filtered to a single job name. It returns the task's current
// declared CPU (MHz) and memory (MB) specs.
func (c *Client) DiscoverTasks(ctx context.Context, namespace, jobFilter string) ([]TaskSpec, error) {
	qOpts := &nomadapi.QueryOptions{
		Namespace: namespace,
	}
	qOpts = qOpts.WithContext(ctx)

	// List all jobs (or a single one if filtered)
	var jobStubs []*nomadapi.JobListStub
	if jobFilter != "" {
		// Fetch just the one job
		job, _, err := c.api.Jobs().Info(jobFilter, qOpts)
		if err != nil {
			return nil, fmt.Errorf("fetching job %q: %w", jobFilter, err)
		}
		stubs, err := c.tasksFromJob(job, namespace)
		if err != nil {
			return nil, err
		}
		return stubs, nil
	}

	// List all jobs in the namespace
	var err error
	jobStubs, _, err = c.api.Jobs().List(qOpts)
	if err != nil {
		return nil, fmt.Errorf("listing jobs: %w", err)
	}

	var tasks []TaskSpec
	for _, stub := range jobStubs {
		// Only consider running jobs
		if stub.Status != "running" {
			continue
		}

		job, _, err := c.api.Jobs().Info(stub.ID, qOpts)
		if err != nil {
			return nil, fmt.Errorf("fetching job %q: %w", stub.ID, err)
		}

		jobTasks, err := c.tasksFromJob(job, namespace)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, jobTasks...)
	}

	return tasks, nil
}

// tasksFromJob extracts TaskSpec entries from a fully-fetched Nomad job.
func (c *Client) tasksFromJob(job *nomadapi.Job, namespace string) ([]TaskSpec, error) {
	if job == nil || job.TaskGroups == nil {
		return nil, nil
	}

	ns := namespace
	if job.Namespace != nil && *job.Namespace != "" {
		ns = *job.Namespace
	}

	var tasks []TaskSpec
	for _, group := range job.TaskGroups {
		if group == nil {
			continue
		}
		for _, task := range group.Tasks {
			if task == nil {
				continue
			}

			spec := TaskSpec{
				Namespace: ns,
				Job:       *job.ID,
				Group:     *group.Name,
				Task:      task.Name,
			}

			if task.Resources != nil {
				if task.Resources.CPU != nil {
					spec.CPUMHz = *task.Resources.CPU
				}
				if task.Resources.MemoryMB != nil {
					spec.MemoryMB = *task.Resources.MemoryMB
				}
			}

			tasks = append(tasks, spec)
		}
	}

	return tasks, nil
}
