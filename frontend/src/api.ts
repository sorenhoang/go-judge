import type {CreateSubmissionResponse, Problem, Submission} from "./types";

const API_BASE_URL = import.meta.env.VITE_API_BASE_URL || "http://localhost:8080";

export async function listProblems(): Promise<Problem[]> {
    const response = await fetch(`${API_BASE_URL}/problems`);
    if(!response.ok) throw new Error(`Failed to fetch problems: ${response.statusText}`);
    return response.json();
}

export async function getProblem(problemId: string): Promise<Problem> {
    const response = await fetch(`${API_BASE_URL}/problems/${problemId}`);
    if(!response.ok) throw new Error(`Failed to fetch problem: ${response.statusText}`);
    return response.json();
}

export async function createSubmission(problemId: string, code: string): Promise<CreateSubmissionResponse> {
    const response = await fetch(`${API_BASE_URL}/submissions`, {
        method: "POST",
        headers: {
            "Content-Type": "application/json",
        },
        body: JSON.stringify({ problemId, code }),
    });
    if(!response.ok) throw new Error(`Failed to create submission: ${response.statusText}`);
    return response.json();
}

export async function getSubmission(submissionId: string): Promise<Submission> {
    const response = await fetch(`${API_BASE_URL}/submissions/${submissionId}`);
    if(!response.ok) throw new Error(`Failed to fetch submission: ${response.statusText}`);
    return response.json();
}