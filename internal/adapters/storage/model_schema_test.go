package storage

import (
	"context"
	"strings"
	"testing"
)

// The first-release schema is installed from an empty database. These checks
// exercise relationships that cannot be inferred from column names alone.
func TestFirstReleaseModelSchema(t *testing.T) {
	for _, backend := range registeredDrivers() {
		t.Run(backend.name, func(t *testing.T) { testFirstReleaseModelSchemaBackend(t, identityStore(t, backend.name)) })
	}
}
func testFirstReleaseModelSchemaBackend(t *testing.T, s *Store) {
	ctx := context.Background()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	reject := func(query string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, query, args...); err == nil {
			t.Fatalf("accepted invalid row: %s", query)
		}
	}
	digest := strings.Repeat("a", 64)
	exec("INSERT INTO workspace(workspace_id,name) VALUES('w','w')")
	exec("INSERT INTO project(project_id,workspace_id,name) VALUES('p',(SELECT id FROM workspace WHERE workspace_id='w'),'p')")
	exec("INSERT INTO scene(workspace_id,project_id,scene_key,name) VALUES((SELECT id FROM workspace WHERE workspace_id='w'),(SELECT id FROM project WHERE project_id='p'),'recovery','Recovery')")
	exec("INSERT INTO scene(workspace_id,project_id,scene_key,name) VALUES((SELECT id FROM workspace WHERE workspace_id='w'),(SELECT id FROM project WHERE project_id='p'),'deployment','Deployment')")
	exec("INSERT INTO policy(policy_id,workspace_id,project_id,scene_key,name) VALUES('policy-1',(SELECT id FROM workspace WHERE workspace_id='w'),(SELECT id FROM project WHERE project_id='p'),'recovery','default')")
	exec("INSERT INTO policy(policy_id,workspace_id,project_id,scene_key,name) VALUES('policy-2',(SELECT id FROM workspace WHERE workspace_id='w'),(SELECT id FROM project WHERE project_id='p'),'deployment','other')")
	reject("UPDATE scene SET default_policy_id=(SELECT id FROM policy WHERE policy_id='policy-2') WHERE scene_key='recovery'")
	exec("INSERT INTO policy_revision(policy_revision_id,policy_id,revision_number,yaml,canonical_json,digest,published_at,published_by,status,updated_by,updated_at) VALUES('rev-1',(SELECT id FROM policy WHERE policy_id='policy-1'),1,'yaml','{}',$1,1,'admin','PUBLISHED','admin',1)", digest)
	exec("UPDATE policy SET active_revision_id=(SELECT id FROM policy_revision WHERE policy_revision_id='rev-1') WHERE policy_id='policy-1'")
	exec("UPDATE scene SET default_policy_id=(SELECT id FROM policy WHERE policy_id='policy-1') WHERE scene_key='recovery'")
	reject("UPDATE policy_revision SET yaml='changed' WHERE policy_revision_id='rev-1'")
	reject("DELETE FROM policy_revision WHERE policy_revision_id='rev-1'")
	reject("INSERT INTO guardrail_run(run_id,workspace_id,project_id,scene_key,policy_id,policy_revision_id,spec_digest,status,change_set_json,change_set_digest,idempotency_key,request_digest,run_ttl_seconds,deadline_at,decision_ttl_seconds,created_by,created_at) VALUES('bad',(SELECT id FROM workspace WHERE workspace_id='w'),(SELECT id FROM project WHERE project_id='p'),'deployment',(SELECT id FROM policy WHERE policy_id='policy-1'),(SELECT id FROM policy_revision WHERE policy_revision_id='rev-1'),$1,'OPEN','{}',$2,'bad',$3,86400,86401,900,'admin',1)", digest, digest, digest)
	exec("INSERT INTO guardrail_run(run_id,workspace_id,project_id,scene_key,policy_id,policy_revision_id,spec_digest,status,change_set_json,change_set_digest,idempotency_key,request_digest,run_ttl_seconds,deadline_at,decision_ttl_seconds,created_by,created_at) VALUES('run-1',(SELECT id FROM workspace WHERE workspace_id='w'),(SELECT id FROM project WHERE project_id='p'),'recovery',(SELECT id FROM policy WHERE policy_id='policy-1'),(SELECT id FROM policy_revision WHERE policy_revision_id='rev-1'),$1,'OPEN','{}',$2,'create-1',$3,86400,86401,900,'admin',1)", digest, digest, digest)
	reject("INSERT INTO submission(submission_id,run_id,submission_no,base_submission_no,idempotency_key,request_digest,payload_json,verdict,summary,finish_run,created_by,created_at) VALUES('invalid',(SELECT id FROM guardrail_run WHERE run_id='run-1'),2,0,'bad',$1,'{}','PASS','x',FALSE,'agent',2)", digest)
	exec("INSERT INTO submission(submission_id,run_id,submission_no,base_submission_no,idempotency_key,request_digest,payload_json,verdict,summary,finish_run,created_by,created_at) VALUES('sub-1',(SELECT id FROM guardrail_run WHERE run_id='run-1'),1,0,'submit-1',$1,'{}','PASS','first',FALSE,'agent',2)", digest)
	exec("INSERT INTO submission(submission_id,run_id,submission_no,base_submission_no,idempotency_key,request_digest,payload_json,verdict,summary,finish_run,created_by,created_at) VALUES('sub-2',(SELECT id FROM guardrail_run WHERE run_id='run-1'),2,1,'submit-2',$1,'{}','PASS','final',TRUE,'agent',3)", digest)
	reject("INSERT INTO submission(submission_id,run_id,submission_no,base_submission_no,idempotency_key,request_digest,payload_json,verdict,summary,finish_run,created_by,created_at) VALUES('duplicate',(SELECT id FROM guardrail_run WHERE run_id='run-1'),3,2,'submit-2',$1,'{}','PASS','duplicate',TRUE,'agent',4)", digest)
	exec("INSERT INTO evidence(submission_id,run_id,check_id,requirement_id,observed_at,query_summary,result_summary,evidence_digest) VALUES((SELECT id FROM submission WHERE submission_id='sub-2'),(SELECT id FROM guardrail_run WHERE run_id='run-1'),'check','requirement','2026-10-26T00:00:00Z','query','result',$1)", digest)
	exec("INSERT INTO evidence(submission_id,run_id,check_id,requirement_id,observed_at,query_summary,result_summary,evidence_digest) VALUES((SELECT id FROM submission WHERE submission_id='sub-2'),(SELECT id FROM guardrail_run WHERE run_id='run-1'),'check','requirement','2026-10-26T00:01:00Z','query','different result',$1)", strings.Repeat("b", 64))
	reject("INSERT INTO evidence(submission_id,run_id,check_id,requirement_id,observed_at,query_summary,result_summary,evidence_digest) VALUES((SELECT id FROM submission WHERE submission_id='sub-2'),(SELECT id FROM guardrail_run WHERE run_id='run-1'),'check','requirement','2026-10-26T00:00:00Z','query','result',$1)", digest)
	reject("UPDATE submission SET summary='changed' WHERE submission_id='sub-2'")
	exec("UPDATE guardrail_run SET status='FINISHED',result_verdict='PASS',decision_verdict='ALLOW',decision_expires_at=3+decision_ttl_seconds,finished_at=3,final_submission_id=(SELECT id FROM submission WHERE submission_id='sub-2'),latest_submission_no=2 WHERE run_id='run-1'")
	reject("UPDATE guardrail_run SET status='EXPIRED',finished_at=NULL,final_submission_id=NULL,expired_at=86401,reason_code='RUN_EXPIRED' WHERE run_id='run-1' AND status='FINISHED'")
}
