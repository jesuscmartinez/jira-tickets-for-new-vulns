package main

import (
	"encoding/json"
	"log"

	"github.com/michael-go/go-jsn/jsn"
)

// IssuesFilter is the top level filter type of filtering Snyk response
type IssuesFilter struct {
	Filters Filter `json:"filters"`
}

type score struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

type Priority struct {
	Score score `json:"score"`
}

// Filter allows to filter on severity, type, ignore or patched vuln
type Filter struct {
	Severities      []string `json:"severities"`
	ExploitMaturity []string `json:"exploitMaturity,omitempty"`
	Priority        Priority `json:"priority"`
	Types           []string `json:"types"`
	Ignored         bool     `json:"ignored"`
	Patched         bool     `json:"patched"`
	isUpgradable    bool     `json:"isUpgradable"`
}

func getVulnsWithoutTicket(flags flags, projectID string, maturityFilter []string, tickets map[string]string, customDebug debug) map[string]interface{} {

	body := IssuesFilter{
		Filter{
			Severities: []string{"high"},
			Types:      []string{"vuln", "license"},
			Priority:   Priority{score{Min: 0, Max: 1000}},
			Ignored:    false,
			Patched:    false,
		},
	}
	if flags.optionalFlags.issueType != "all" && flags.optionalFlags.issueType != "" {
		body.Filters.Types = []string{flags.optionalFlags.issueType}
	}
	switch flags.optionalFlags.severity {
	case "critical":
		body.Filters.Severities = []string{"critical"}
	case "high":
		body.Filters.Severities = []string{"critical", "high"}
	case "medium":
		body.Filters.Severities = []string{"critical", "high", "medium"}
	case "low":
		body.Filters.Severities = []string{"critical", "high", "medium", "low"}
	default:
		log.Fatalln("Unexpected severity threshold")
	}
	if len(maturityFilter) > 0 {
		body.Filters.ExploitMaturity = maturityFilter
	}

	body.Filters.Priority.Score.Min = 0
	body.Filters.Priority.Score.Max = 1000
	if flags.optionalFlags.priorityScoreThreshold > 0 {
		body.Filters.Priority.Score.Min = flags.optionalFlags.priorityScoreThreshold
	}

	marshalledBody, err := json.Marshal(body)

	if err != nil {
		log.Fatalln(err)
	}

	responseAggregatedData, err := makeSnykAPIRequest("POST", flags.mandatoryFlags.endpointAPI+"/v1/org/"+flags.mandatoryFlags.orgID+"/project/"+projectID+"/aggregated-issues", flags.mandatoryFlags.apiToken, marshalledBody, customDebug)
	if err != nil {
		log.Printf("*** ERROR *** Could not get aggregated data from %s org %s project %s", flags.mandatoryFlags.endpointAPI, flags.mandatoryFlags.orgID, projectID)
		log.Fatalln(err)
	}

	j, err := jsn.NewJson(responseAggregatedData)
	vulnsWithAllPaths := make(map[string]interface{})

	issueType := ""
	listOfIssues := j.K("issues").Array().Elements()
	if len(listOfIssues) > 0 {
		issueType = listOfIssues[0].K("issueType").String().Value
	}

	// IAC issues are of type configuration and are not supported atm
	if issueType == "configuration" {
		customDebug.Debug(" *** INFO *** IAC projects are not supported by this tool, skipping this project")
		return vulnsWithAllPaths
	}

	// Code issue
	// the response from aggregated data is empty for code issues
	if len(listOfIssues) == 0 {
		return getSnykCodeIssueWithoutTickets(flags, projectID, tickets, customDebug)
	}

	// Open source issue
	vulnsWithAllPaths = getSnykOpenSourceIssueWithoutTickets(flags, projectID, maturityFilter, tickets, customDebug, responseAggregatedData)

	return vulnsWithAllPaths
}

/***
function getSnykOpenSourceIssueWithoutTickets
input flags mandatory and optionnal flags
input projectID string, the ID of the project we are get issues from
input tickets map[string]string, the list value pair ticket id, issue id which already have a ticket
input debug customDebug
input responseAggregatedData []byte, response from the aggregated data endpoint
return vulnsWithAllPaths map[string]interface{}, list of issues with all details and path
Create a list of issue details without tickets.
	Loop through the issues
		Get the path for each issue id
		add the path to the issue details
***/
func getSnykOpenSourceIssueWithoutTickets(flags flags, projectID string, maturityFilter []string, tickets map[string]string, customDebug debug, responseAggregatedData []byte) map[string]interface{} {

	vulnsPerPath := make(map[string]interface{})
	vulnsWithAllPaths := make(map[string]interface{})

	j, err := jsn.NewJson(responseAggregatedData)
	if err != nil {
		log.Fatalln(err)
	}

	listOfIssues := j.K("issues").Array().Elements()

	for _, e := range listOfIssues {
		if e.K("issueType").String().Value == "vuln" {
			if len(e.K("id").String().Value) != 0 {
				if _, found := tickets[e.K("id").String().Value]; !found {
					var issueId = e.K("id").String().Value

					bytes, err := json.Marshal(e)
					if err != nil {
						log.Fatalln(err)
					}
					json.Unmarshal(bytes, &vulnsPerPath)

					ProjectIssuePathData, err := makeSnykAPIRequest("GET", flags.mandatoryFlags.endpointAPI+"/v1/org/"+flags.mandatoryFlags.orgID+"/project/"+projectID+"/issue/"+issueId+"/paths", flags.mandatoryFlags.apiToken, nil, customDebug)
					if err != nil {
						log.Printf("*** ERROR *** Could not get aggregated data from %s org %s project %s issue %s", flags.mandatoryFlags.endpointAPI, flags.mandatoryFlags.orgID, projectID, issueId)
						log.Fatalln(err)
					}
					ProjectIssuePathDataJson, er := jsn.NewJson(ProjectIssuePathData)
					if er != nil {
						log.Printf("*** ERROR *** Json creation failed\n")
						log.Fatalln(er)
					}
					vulnsPerPath["from"] = ProjectIssuePathDataJson.K("paths")
					marshalledvulnsPerPath, err := json.Marshal(vulnsPerPath)
					vulnsWithAllPaths[issueId], err = jsn.NewJson(marshalledvulnsPerPath)
					if er != nil {
						log.Printf("*** ERROR *** Json creation failed\n")
						log.Fatalln(er)
					}
				}
			}
		}
	}

	for _, e := range j.K("issues").K("licenses").Array().Elements() {
		if e.K("id").String().Value != "" {
			if _, found := tickets[e.K("id").String().Value]; !found {
				var issueId = e.K("id").String().Value

				bytes, err := json.Marshal(e)
				if err != nil {
					log.Fatalln(err)
				}
				json.Unmarshal(bytes, &vulnsPerPath)

				ProjectIssuePathData, err := makeSnykAPIRequest("GET", flags.mandatoryFlags.endpointAPI+"/v1/org/"+flags.mandatoryFlags.orgID+"/project/"+projectID+"/issue/"+issueId+"/paths", flags.mandatoryFlags.apiToken, nil, customDebug)
				if err != nil {
					log.Printf("*** ERROR *** Could not get aggregated data from %s org %s project %s issue %s", flags.mandatoryFlags.endpointAPI, flags.mandatoryFlags.orgID, projectID, issueId)
					log.Fatalln(err)
				}
				ProjectIssuePathDataJson, er := jsn.NewJson(ProjectIssuePathData)
				if er != nil {
					log.Printf("*** ERROR *** Json creation failed\n")
					log.Fatalln(er)
				}
				vulnsPerPath["from"] = ProjectIssuePathDataJson.K("paths")
				marshalledvulnsPerPath, err := json.Marshal(vulnsPerPath)
				vulnsWithAllPaths[issueId], err = jsn.NewJson(marshalledvulnsPerPath)
				if er != nil {
					log.Printf("*** ERROR *** Json creation failed\n")
					log.Fatalln(er)
				}
			}
		}
	}
	return vulnsWithAllPaths
}

func createMaturityFilter(filtersArray []string) []string {

	var MaturityFilter []string

	for _, filter := range filtersArray {
		switch filter {
		case "no-data":
			MaturityFilter = append(MaturityFilter, filter)
		case "no-known-exploit":
			MaturityFilter = append(MaturityFilter, filter)
		case "proof-of-concept":
			MaturityFilter = append(MaturityFilter, filter)
		case "mature":
			MaturityFilter = append(MaturityFilter, filter)
		case "":
		default:
			log.Fatalf("*** ERROR ***: %s is not a valid maturity level. Levels are Must be one of [no-data,no-known-exploit,proof-of-concept,mature]", filter)
		}
	}
	return MaturityFilter
}

/***
function getSnykCodeIssueWithoutTickets
input flags mandatory and optionnal flags
input projectID string, the ID of the project we are get issues from
input tickets map[string]string, the list value pair ticket id, issue id which already have a ticket
input debug customDebug
return fullCodeIssueDetail map[string]interface{}, list of issue details without tickets
Create a list of issue details without tickets.
	Loop through the severity array to get the all issues IDs
	Loop through those ids the get the details
		The issue details doesn't give the title of the severity => adding it the the details
	Adding all the details to the list
***/
func getSnykCodeIssueWithoutTickets(flags flags, projectID string, tickets map[string]string, customDebug debug) map[string]interface{} {

	// In the v1 api low severity means get all the issues up,
	// mediun means all but low and so on
	// this is not possible with v3.
	// to keep the logic of the tool
	// we create an array of severity
	// and loop on it to get all the issues
	severity := []string{}
	switch flags.optionalFlags.severity {
	case "critical":
		severity = []string{"critical"}
	case "high":
		severity = []string{"critical", "high"}
	case "medium":
		severity = []string{"critical", "high", "medium"}
	case "low":
		severity = []string{"critical", "high", "medium", "low"}
	default:
		log.Fatalln("Unexpected severity threshold")
	}

	fullCodeIssueDetail := make(map[string]interface{})

	// Doing this for test propose
	endpointAPI := "https://api.snyk.io"
	if IsTestRun() {
		endpointAPI = flags.mandatoryFlags.endpointAPI
	}

	for _, severityIndexValue := range severity {

		url := endpointAPI + "/v3/orgs/" + flags.mandatoryFlags.orgID + "/issues?project_id=" + projectID + "&version=2021-08-20~experimental"
		if len(flags.optionalFlags.severity) > 0 {
			url = endpointAPI + "/v3/orgs/" + flags.mandatoryFlags.orgID + "/issues?project_id=" + projectID + "&severity=" + severityIndexValue + "&version=2021-08-20~experimental"
		}

		for {

			// get the list of code issue for this project
			responseData, err := makeSnykAPIRequest("GET", url, flags.mandatoryFlags.apiToken, nil, customDebug)

			if err != nil {
				if err.Error() != "Not found, Request failed" {
					log.Printf("*** ERROR *** Could not get code issues list from %s org %s project %s", flags.mandatoryFlags.endpointAPI, flags.mandatoryFlags.orgID, projectID)
					log.Fatal()
				}
			}

			// loop through the issues and get the details
			jsonData, err := jsn.NewJson(responseData)

			issueDetail := make(map[string]interface{})

			for _, e := range jsonData.K("data").Array().Elements() {

				if len(e.K("id").String().Value) != 0 {
					if _, found := tickets[e.K("id").String().Value]; !found {

						id := e.K("id").String().Value

						url := endpointAPI + "/v3/orgs/" + flags.mandatoryFlags.orgID + "/issues/detail/code/" + id + "?project_id=" + projectID + "&version=2021-08-20~experimental"

						// get the details of this code issue id
						responseIssueDetail, err := makeSnykAPIRequest("GET", url, flags.mandatoryFlags.apiToken, nil, customDebug)
						if err != nil {
							log.Printf("*** ERROR *** Could not get code issues list from %s org %s project %s", flags.mandatoryFlags.endpointAPI, flags.mandatoryFlags.orgID, projectID)
							log.Fatalln(err)
						}

						jsonIssueDetail, er := jsn.NewJson(responseIssueDetail)
						if er != nil {
							log.Printf("*** ERROR *** Json creation failed\n")
							log.Fatalln(er)
						}

						bytes, err := json.Marshal(jsonIssueDetail)
						if err != nil {
							log.Fatalln(err)
						}
						json.Unmarshal(bytes, &issueDetail)

						issueDetail["title"] = e.K("attributes").K("title").String().Value

						marshalledjsonIssueDetail, err := json.Marshal(issueDetail)
						fullCodeIssueDetail[id], err = jsn.NewJson(marshalledjsonIssueDetail)
						if er != nil {
							log.Printf("*** ERROR *** Json creation failed\n")
							log.Fatalln(er)
						}
					}

				}

			}

			if len(jsonData.K("links").K("next").String().Value) > 0 {
				url = endpointAPI + jsonData.K("links").K("next").String().Value
			} else {
				break
			}
		}
	}

	return fullCodeIssueDetail
}
