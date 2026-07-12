export type Problem = {
  id: string;
  title: string;
  description: string;
  difficulty: string;
  starter_code: string;
  created_at: string;
  updated_at: string;
};

export type SubmissionStatus =
  | "PENDING"
  | "RUNNING"
  | "PASSED"
  | "FAILED"
  | "ERROR";

export type Submission = {
  id: string;
  problem_id: string;
  code: string;
  status: SubmissionStatus;
  output: string | null;
  total_tests: number;
  passed_tests: number;
  created_at: string;
  updated_at: string;
};

export type CreateSubmissionResponse = {
  submissionId: string;
  status: SubmissionStatus;
};