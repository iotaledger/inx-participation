package participation

import (
	"net/http"
	
	"github.com/labstack/echo/v4"

	restapipkg "github.com/gohornet/hornet/pkg/restapi"
)

const (
	APIRoute = "participation/v1"

	// RouteParticipationEvents is the route to list all events, returning their ID, the event name and status.
	// GET returns a list of all events known to the node. Optional query parameter returns filters events by type (query parameters: "type").
	RouteParticipationEvents = "/events"

	// RouteParticipationEvent is the route to access a single participation by its ID.
	// GET gives a quick overview of the participation. This does not include the current standings.
	RouteParticipationEvent = "/events/:" + ParameterParticipationEventID

	// RouteParticipationEventStatus is the route to access the status of a single participation by its ID.
	// GET returns the amount of tokens participating and accumulated votes for the ballot if the event contains a ballot. Optional query parameter returns the status for the given milestone index (query parameters: "milestoneIndex").
	RouteParticipationEventStatus = "/events/:" + ParameterParticipationEventID + "/status"

	// RouteOutputStatus is the route to get the vote status for a given outputID.
	// GET returns the messageID the participation was included, the starting and ending milestone index this participation was tracked.
	RouteOutputStatus = "/outputs/:" + restapipkg.ParameterOutputID

	// RouteAddressBech32Status is the route to get the staking rewards for the given bech32 address.
	RouteAddressBech32Status = "/addresses/:" + restapipkg.ParameterAddress

	// RouteAddressBech32Outputs is the route to get the outputs for the given bech32 address.
	RouteAddressBech32Outputs = "/addresses/:" + restapipkg.ParameterAddress + "/outputs"

	// RouteAdminCreateEvent is the route the node operator can use to add events.
	// POST creates a new event to track
	RouteAdminCreateEvent = "/admin/events"

	// RouteAdminDeleteEvent is the route the node operator can use to remove events.
	// DELETE removes a tracked participation.
	RouteAdminDeleteEvent = "/admin/events/:" + ParameterParticipationEventID

	// RouteAdminActiveParticipations is the route the node operator can use to get all the active participations for a certain event.
	// GET returns a list of all active participations
	RouteAdminActiveParticipations = "/admin/events/:" + ParameterParticipationEventID + "/active"

	// RouteAdminPastParticipations is the route the node operator can use to get all the past participations for a certain event.
	// GET returns a list of all past participations
	RouteAdminPastParticipations = "/admin/events/:" + ParameterParticipationEventID + "/past"

	// RouteAdminRewards is the route the node operator can use to get the rewards for a staking event.
	// GET retrieves the staking event rewards.
	RouteAdminRewards = "/admin/events/:" + ParameterParticipationEventID + "/rewards"

	// ParameterParticipationEventID is used to identify an event by its ID.
	ParameterParticipationEventID = "eventID"
)

func setupRoutes(e *echo.Echo) {
	routeGroup := e.Group(APIRoute)

	routeGroup.GET(RouteParticipationEvents, func(c echo.Context) error {
		resp, err := getEvents(c)
		if err != nil {
			return err
		}

		return restapipkg.JSONResponse(c, http.StatusOK, resp)
	})

	routeGroup.POST(RouteAdminCreateEvent, func(c echo.Context) error {

		resp, err := createEvent(c)
		if err != nil {
			return err
		}

		c.Response().Header().Set(echo.HeaderLocation, resp.EventID)
		return restapipkg.JSONResponse(c, http.StatusCreated, resp)
	})

	routeGroup.GET(RouteParticipationEvent, func(c echo.Context) error {
		resp, err := getEvent(c)
		if err != nil {
			return err
		}

		return restapipkg.JSONResponse(c, http.StatusOK, resp)
	})

	routeGroup.DELETE(RouteAdminDeleteEvent, func(c echo.Context) error {
		if err := deleteEvent(c); err != nil {
			return err
		}
		return c.NoContent(http.StatusNoContent)
	})

	routeGroup.GET(RouteParticipationEventStatus, func(c echo.Context) error {
		resp, err := getEventStatus(c)
		if err != nil {
			return err
		}

		return restapipkg.JSONResponse(c, http.StatusOK, resp)
	})

	routeGroup.GET(RouteOutputStatus, func(c echo.Context) error {
		resp, err := getOutputStatus(c)
		if err != nil {
			return err
		}
		return restapipkg.JSONResponse(c, http.StatusOK, resp)
	})

	routeGroup.GET(RouteAddressBech32Status, func(c echo.Context) error {
		resp, err := getRewardsByAddress(c)
		if err != nil {
			return err
		}
		return restapipkg.JSONResponse(c, http.StatusOK, resp)
	})

	routeGroup.GET(RouteAddressBech32Outputs, func(c echo.Context) error {
		resp, err := getOutputsByAddress(c)
		if err != nil {
			return err
		}
		return restapipkg.JSONResponse(c, http.StatusOK, resp)
	})

	routeGroup.GET(RouteAdminActiveParticipations, func(c echo.Context) error {
		resp, err := getActiveParticipations(c)
		if err != nil {
			return err
		}
		return restapipkg.JSONResponse(c, http.StatusOK, resp)
	})

	routeGroup.GET(RouteAdminPastParticipations, func(c echo.Context) error {
		resp, err := getPastParticipations(c)
		if err != nil {
			return err
		}
		return restapipkg.JSONResponse(c, http.StatusOK, resp)
	})

	routeGroup.GET(RouteAdminRewards, func(c echo.Context) error {
		resp, err := getRewards(c)
		if err != nil {
			return err
		}
		return restapipkg.JSONResponse(c, http.StatusOK, resp)
	})
}
