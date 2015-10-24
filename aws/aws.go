package aws

import (
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/awslabs/aws-sdk-go/aws/credentials/ec2rolecreds"
	"github.com/bobtfish/AWSnycast/healthcheck"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

type MyEC2Conn interface {
	CreateRoute(*ec2.CreateRouteInput) (*ec2.CreateRouteOutput, error)
	ReplaceRoute(*ec2.ReplaceRouteInput) (*ec2.ReplaceRouteOutput, error)
	DescribeRouteTables(*ec2.DescribeRouteTablesInput) (*ec2.DescribeRouteTablesOutput, error)
	DeleteRoute(*ec2.DeleteRouteInput) (*ec2.DeleteRouteOutput, error)
}

type MetadataFetcher interface {
	Available() bool
	GetMetadata(string) (string, error)
}

func NewMetadataFetcher(debug bool) MetadataFetcher {
	c := ec2metadata.Config{}
	if debug {
		c.LogLevel = aws.LogLevel(aws.LogDebug)
	}
	return ec2metadata.New(&c)
}

type ManageRoutesSpec struct {
	Cidr            string `yaml:"cidr"`
	Instance        string `yaml:"instance"`
	InstanceIsSelf  bool   `yaml:"-"`
	HealthcheckName string `yaml:"healthcheck"`
	healthcheck     healthcheck.CanBeHealthy
	IfUnhealthy     bool `yaml:"if_unhealthy"`
	ec2RouteTables  []*ec2.RouteTable
	Manager         RouteTableFetcher `yaml:"-"`
}

func (r *ManageRoutesSpec) Default(instance string) {
	if !strings.Contains(r.Cidr, "/") {
		r.Cidr = fmt.Sprintf("%s/32", r.Cidr)
	}
	if r.Instance == "" {
		r.Instance = "SELF"
	}
	if r.Instance == "SELF" {
		r.InstanceIsSelf = true
		r.Instance = instance
	}
	r.ec2RouteTables = make([]*ec2.RouteTable, 0)
}

func (r *ManageRoutesSpec) Validate(name string, healthchecks map[string]*healthcheck.Healthcheck) error {
	if r.Cidr == "" {
		return errors.New(fmt.Sprintf("cidr is not defined in %s", name))
	}
	if _, _, err := net.ParseCIDR(r.Cidr); err != nil {
		return errors.New(fmt.Sprintf("Could not parse %s in %s", err.Error(), name))
	}
	if r.HealthcheckName != "" {
		hc, ok := healthchecks[r.HealthcheckName]
		if !ok {
			return errors.New(fmt.Sprintf("Route table %s, upsert %s cannot find healthcheck '%s'", name, r.Cidr, r.HealthcheckName))
		}
		r.healthcheck = hc
	}
	return nil
}

func (r *ManageRoutesSpec) StartHealthcheckListener(noop bool) {
	if r.healthcheck == nil {
		return
	}
	go func() {
		c := r.healthcheck.GetListener()
		for {
			resText := "FAILED"
			if <-c {
				resText = "PASSED"
			}
			log.Printf("Got notification from healthcheck %s: %s, kicking routes for %s", r.HealthcheckName, resText, r.Cidr)
			for _, rtb := range r.ec2RouteTables {
				log.Printf("RTB IN KICK: %+v", rtb)
				if err := r.Manager.ManageInstanceRoute(*rtb, *r, noop); err != nil {
					log.Printf("ERROR: %s", err.Error())
				}
			}
		}
	}()
	return
}

func (r *ManageRoutesSpec) UpdateEc2RouteTables(rt []*ec2.RouteTable) {
	r.ec2RouteTables = rt
}

type RouteTableFetcher interface {
	GetRouteTables() ([]*ec2.RouteTable, error)
	ManageInstanceRoute(ec2.RouteTable, ManageRoutesSpec, bool) error
}

type RouteTableFetcherEC2 struct {
	Region string
	conn   MyEC2Conn
}

func getCreateRouteInput(rtb ec2.RouteTable, cidr string, instance string, noop bool) ec2.CreateRouteInput {
	return ec2.CreateRouteInput{
		RouteTableId:         rtb.RouteTableId,
		DestinationCidrBlock: aws.String(cidr),
		InstanceId:           aws.String(instance),
		DryRun:               aws.Bool(noop),
	}
}

func (r RouteTableFetcherEC2) ManageInstanceRoute(rtb ec2.RouteTable, rs ManageRoutesSpec, noop bool) error {
	route := findRouteFromRouteTable(rtb, rs.Cidr)
	if route != nil {
		if route.InstanceId != nil && *(route.InstanceId) == rs.Instance {
			if rs.HealthcheckName != "" && !rs.healthcheck.IsHealthy() && rs.healthcheck.CanPassYet() {
				log.Printf("[INFO] Deleting route for %s: %s %s", *rtb.RouteTableId, rs.Cidr, rs.Instance)
				if err := r.DeleteInstanceRoute(rtb.RouteTableId, route, rs.Cidr, rs.Instance, noop); err != nil {
					return err
				}
				return nil
			}
			log.Printf("Skipping doing anything, %s is already routed via %s", rs.Cidr, rs.Instance)
			return nil
		}

		if err := r.ReplaceInstanceRoute(rtb.RouteTableId, route, rs.Cidr, rs.Instance, rs.IfUnhealthy, noop); err != nil {
			return err
		}
		return nil
	}
	if rs.HealthcheckName != "" && !rs.healthcheck.IsHealthy() {
		return nil
	}

	opts := getCreateRouteInput(rtb, rs.Cidr, rs.Instance, noop)

	log.Printf("[INFO] Creating route for %s: %#v", *rtb.RouteTableId, opts)
	if _, err := r.conn.CreateRoute(&opts); err != nil {
		return err
	}
	return nil
}

func findRouteFromRouteTable(rtb ec2.RouteTable, cidr string) *ec2.Route {
	for _, route := range rtb.Routes {
		if *(route.DestinationCidrBlock) == cidr {
			return route
		}
	}
	return nil
}

func (r RouteTableFetcherEC2) DeleteInstanceRoute(routeTableId *string, route *ec2.Route, cidr string, instance string, noop bool) error {
	params := &ec2.DeleteRouteInput{
		DestinationCidrBlock: aws.String(cidr),
		RouteTableId:         routeTableId,
		DryRun:               aws.Bool(noop),
	}
	resp, err := r.conn.DeleteRoute(params)
	if err != nil {
		// Print the error, cast err to awserr.Error to get the Code and
		// Message from an error.
		fmt.Println(err.Error())
		return err
	}
	fmt.Println(resp)
	return nil
}

func (r RouteTableFetcherEC2) ReplaceInstanceRoute(routeTableId *string, route *ec2.Route, cidr string, instance string, ifUnhealthy bool, noop bool) error {
	params := &ec2.ReplaceRouteInput{
		DestinationCidrBlock: aws.String(cidr),
		RouteTableId:         routeTableId,
		InstanceId:           aws.String(instance),
		DryRun:               aws.Bool(noop),
	}
	if ifUnhealthy && *(route.State) == "active" {
		log.Printf("Not replacing route, as current route is active/healthy")
		return nil
	}
	resp, err := r.conn.ReplaceRoute(params)
	if err != nil {
		// Print the error, cast err to awserr.Error to get the Code and
		// Message from an error.
		fmt.Println(err.Error())
		return err
	}
	fmt.Println(resp)
	return nil
}

func (r RouteTableFetcherEC2) GetRouteTables() ([]*ec2.RouteTable, error) {
	resp, err := r.conn.DescribeRouteTables(&ec2.DescribeRouteTablesInput{})
	if err != nil {
		log.Printf("Error on DescribeRouteTables: %s", err)
		return []*ec2.RouteTable{}, err
	}
	return resp.RouteTables, nil
}

func getProviders() []credentials.Provider {
	return []credentials.Provider{
		&credentials.EnvProvider{},
		&ec2rolecreds.EC2RoleProvider{
			Client: ec2metadata.New(&ec2metadata.Config{
				HTTPClient: &http.Client{
					Timeout: 2 * time.Second,
				},
			}),
		},
	}
}

func getCred(providers []credentials.Provider) *credentials.Credentials {
	cred := credentials.NewChainCredentials(providers)
	_, credErr := cred.Get()
	if credErr != nil {
		panic(credErr)
	}
	return cred
}

func NewRouteTableFetcher(region string, debug bool) RouteTableFetcher {
	r := RouteTableFetcherEC2{}
	providers := getProviders()
	cred := getCred(providers)
	awsConfig := &aws.Config{
		Credentials: cred,
		Region:      aws.String(region),
		MaxRetries:  aws.Int(3),
	}
	r.conn = ec2.New(awsConfig)
	return r
}

type RouteTableFilter interface {
	Keep(*ec2.RouteTable) bool
}

type RouteTableFilterAlways struct{}

func (fs RouteTableFilterAlways) Keep(rt *ec2.RouteTable) bool {
	return false
}

type RouteTableFilterNever struct{}

func (fs RouteTableFilterNever) Keep(rt *ec2.RouteTable) bool {
	return true
}

type RouteTableFilterAnd struct {
	RouteTableFilters []RouteTableFilter
}

func (fs RouteTableFilterAnd) Keep(rt *ec2.RouteTable) bool {
	for _, f := range fs.RouteTableFilters {
		if !f.Keep(rt) {
			return false
		}
	}
	return true
}

type RouteTableFilterOr struct {
	RouteTableFilters []RouteTableFilter
}

func (fs RouteTableFilterOr) Keep(rt *ec2.RouteTable) bool {
	for _, f := range fs.RouteTableFilters {
		if f.Keep(rt) {
			return true
		}
	}
	return false
}

type RouteTableFilterMain struct{}

func (fs RouteTableFilterMain) Keep(rt *ec2.RouteTable) bool {
	for _, a := range rt.Associations {
		if *(a.Main) {
			return true
		}
	}
	return false
}

func FilterRouteTables(f RouteTableFilter, tables []*ec2.RouteTable) []*ec2.RouteTable {
	out := make([]*ec2.RouteTable, 0, len(tables))
	for _, rtb := range tables {
		if f.Keep(rtb) {
			out = append(out, rtb)
		}
	}
	return out
}

func RouteTableForSubnet(subnet string, tables []*ec2.RouteTable) *ec2.RouteTable {
	subnet_rtb := FilterRouteTables(RouteTableFilterSubnet{SubnetId: subnet}, tables)
	if len(subnet_rtb) == 0 {
		main_rtbs := FilterRouteTables(RouteTableFilterMain{}, tables)
		if len(main_rtbs) == 0 {
			return nil
		}
		return main_rtbs[0]
	}
	return subnet_rtb[0]
}

type RouteTableFilterSubnet struct {
	SubnetId string
}

func (fs RouteTableFilterSubnet) Keep(rt *ec2.RouteTable) bool {
	for _, a := range rt.Associations {
		if a.SubnetId != nil && *(a.SubnetId) == fs.SubnetId {
			return true
		}
	}
	return false
}

type RouteTableFilterDestinationCidrBlock struct {
	DestinationCidrBlock string
	ViaIGW               bool
	ViaInstance          bool
	InstanceNotActive    bool
}

func (fs RouteTableFilterDestinationCidrBlock) Keep(rt *ec2.RouteTable) bool {
	for _, r := range rt.Routes {
		if r.DestinationCidrBlock != nil && *(r.DestinationCidrBlock) == fs.DestinationCidrBlock {
			if fs.ViaIGW {
				if r.GatewayId != nil && strings.HasPrefix(*(r.GatewayId), "igw-") {
					return true
				}
			} else {
				if fs.ViaInstance {
					if r.InstanceId != nil {
						if fs.InstanceNotActive {
							if *(r.State) != "active" {
								return true
							}
						} else {
							return true
						}
					}
				} else {
					return true
				}
			}
		}
	}
	return false
}

type RouteTableFilterTagMatch struct {
	Key   string
	Value string
}

func (fs RouteTableFilterTagMatch) Keep(rt *ec2.RouteTable) bool {
	for _, t := range rt.Tags {
		if *(t.Key) == fs.Key && *(t.Value) == fs.Value {
			return true
		}
	}
	return false
}
