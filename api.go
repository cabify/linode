package linode

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const (
	apiEndpoint      = "https://api.linode.com/"
	maxBatchRequests = 24
)

var apiEndpointURL *url.URL

func init() {
	apiEndpointURL, _ = url.Parse(apiEndpoint)
}

// NewClient creates a client instance which can be used to craft
// HTTP requests and parse JSON responses from the Linode API.
func NewClient(apiKey string) *Client {
	return &Client{apiKey: apiKey}
}

type Client struct {
	apiKey string
}

func (c *Client) NewRequest() *request {
	return &request{client: *c}
}

type request struct {
	client  Client
	actions []action
}

type action map[string]string

// AddActions adds an API action to the request. This corresponds to the 'api_action' parameter.
// The returned object can be modified to include additional API parameters. See #Set.
func (r *request) AddAction(method string, params map[string]string) *request {
	var a action
	if params == nil {
		a = make(action)
	} else {
		a = action(params)
	}
	a["api_action"] = method
	r.actions = append(r.actions, a)
	return r
}

func (r *request) URLs() ([]string, error) {
	numActions := len(r.actions)
	if numActions == 0 {
		return []string{}, nil
	}
	// divide the actions into groups which respect the max number of batch actions
	numerator := maxBatchRequests + 1
	numBatches := (numActions / numerator) + 1
	actionBatches := make([][]action, numBatches)
	for i, action := range r.actions {
		j := i / numerator
		actionBatches[j] = append(actionBatches[j], action)
	}

	// create a url for each batch request
	urls := make([]string, len(actionBatches))
	for i, actions := range actionBatches {
		params := make(url.Values)
		params.Set("api_key", r.client.apiKey)
		params.Set("api_action", "batch")
		requestArrayValue, err := json.Marshal(actions)
		if err != nil {
			return nil, err
		}
		params.Set("api_requestArray", string(requestArrayValue))
		u := apiEndpointURL // make a copy of the base URL
		u.RawQuery = params.Encode()
		urls[i] = u.String()
	}
	return urls, nil
}

type response struct {
	Action string
	Data   json.RawMessage
}

func (r *request) GetJSON() ([]response, error) {
	var responses []response
	var errs []error

	urls, err := r.URLs()
	if err != nil {
		return nil, err
	}
	for _, u := range urls {
		responses, errs = getJSON(u, responses, errs)
	}
	if len(errs) > 0 {
		errStrings := make([]string, len(errs))
		for i, err := range errs {
			errStrings[i] = err.Error()
		}
		return nil, errors.New(strings.Join(errStrings, "; "))
	}
	return responses, nil
}

func getJSON(u string, responses []response, errs []error) ([]response, []error) {
	resp, err := http.Get(u)
	if err != nil {
		errs = append(errs, err)
		return responses, errs
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		errs = append(errs, fmt.Errorf("HTTP error: %s", resp.Status))
		return responses, errs
	}

	decoder := json.NewDecoder(resp.Body)

	var responseJSONs []responseJSON
	if err = decoder.Decode(&responseJSONs); err != nil {
		errs = append(errs, fmt.Errorf("unable to decode api JSON response"))
		return responses, errs
	}

	for _, r := range responseJSONs {
		// Check for 'ERROR' attribute for any values, which would indicate an error
		if len(r.Errors) > 0 {
			for _, e := range r.Errors {
				errs = append(errs, fmt.Errorf("[code: %d] %s", e.Code, e.Message))
			}
			continue
		}
		responses = append(responses, response{Action: r.Action, Data: r.Data})
	}
	return responses, errs
}

type responseJSON struct {
	Action string `json:"ACTION"`
	Errors []struct {
		Code    int    `json:"ERRORCODE"`
		Message string `json:"ERRORMESSAGE"`
	} `json:"ERRORARRAY,omitempty"`
	Data json.RawMessage `json:"DATA,omitempty"`
}