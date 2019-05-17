package main

import (
	"database/sql"
        "encoding/json"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/elasticsearchservice"
	"github.com/go-martini/martini"
	_ "github.com/lib/pq"
	"github.com/martini-contrib/binding"
	"github.com/martini-contrib/render"
	"github.com/nu7hatch/gouuid"
	"os"
	vault "github.com/akkeris/vault-client"
	"strings"
)

var svc *elasticsearchservice.ElasticsearchService
var region string
var plans map[string]string
var brokerdb_pool *sql.DB

type tagspec struct {
	Resource string `json:"resource"`
	Name     string `json:"name"`
	Value    string `json:"value"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type provisionspec struct {
	Plan        string `json:"plan"`
	Billingcode string `json:"billingcode"`
}

type ESSpec struct {
	ES_URL     string `json:"ES_URL"`
	KIBANA_URL string `json:"KIBANA_URL"`
}

func vaulthelper(secret vault.VaultSecret, which string) (v string) {
	for _, element := range secret.Fields {
		if element.Key == which {
			return element.Value
		}
	}
	return ""
}

func setenv() {
	plans = make(map[string]string)
	plans["micro"] = "Micro - 1xCPU - 2 GB RAM - 10 GB Disk"
	plans["small"] = "Small - 2xCPU - 4 GB RAM - 20 GB Disk"
	plans["medium"] = "Medium - 2xCPU - 8 GB RAM - 40 GB Disk - Encryption at Rest"
	plans["large"] = "Large - 4xCPU - 16 GB RAM - 80 GB Disk - Encryption at Rest"
        plans["premium-0"] = "Production - 3 masters - 4 data nodes - multi-AZ - 2xCPU - 8 GB RAM - 100 GB Disk each - Encryption at Rest"
	region = os.Getenv("REGION")
	svc = elasticsearchservice.New(session.New(&aws.Config{
		Region: aws.String(region),
	}))
	secret := vault.GetSecret(os.Getenv("BROKERDB_SECRET"))
	_brokerdb := vaulthelper(secret, "location")
	var err error
	brokerdb_pool, err = getDB(_brokerdb)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	err = createdb()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
func main() {
	setenv()
	m := martini.Classic()
	m.Use(render.Renderer())
	m.Post("/v1/es/instance", binding.Json(provisionspec{}), provision_handler)
	m.Get("/v1/es/instance/:domainname/status", status_handler)
	m.Get("/v1/es/plans", plans_handler)
	m.Get("/v1/es/url/:domainname", status_handler)
	m.Delete("/v1/es/instance/:domainname", delete_handler)
	m.Post("/v1/es/tag", binding.Json(tagspec{}), tag_handler)
	m.Run()

}

func delete_handler(params martini.Params, r render.Render) {
	domainname := params["domainname"]
	err := delete(domainname)
	if err != nil {
		fmt.Println(err)
		errorout := make(map[string]interface{})
		errorout["error"] = err
		r.JSON(500, errorout)
		return
	}
	r.JSON(200, map[string]string{"message": "deleted"})

}

func delete(domainname string) (e error) {
	params := &elasticsearchservice.DeleteElasticsearchDomainInput{
		DomainName: aws.String(domainname), // Required
	}
	_, err := svc.DeleteElasticsearchDomain(params)

	if err != nil {
		return err
	}
	err = deletefromdb(domainname)
	if err != nil {
		return err
	}
	return nil
}

func tag_handler(spec tagspec, berr binding.Errors, r render.Render) {
	if berr != nil {
		fmt.Println(berr)
		errorout := make(map[string]interface{})
		errorout["error"] = berr
		r.JSON(500, errorout)
		return
	}
	err := tag(spec)
	if err != nil {
		fmt.Println(err)
		errorout := make(map[string]interface{})
		errorout["error"] = err
		r.JSON(500, errorout)
		return
	}
	r.JSON(201, map[string]interface{}{"response": "tag added"})

}
func tag(spec tagspec) (e error) {
	accountnumber := os.Getenv("ACCOUNTNUMBER")
	name := spec.Resource

	arnname := "arn:aws:es:" + region + ":" + accountnumber + ":domain/" + name

	params := &elasticsearchservice.AddTagsInput{
		ARN: aws.String(arnname),
		TagList: []*elasticsearchservice.Tag{ // Required
			{
				Key:   aws.String(spec.Name),
				Value: aws.String(spec.Value),
			},
		},
	}
	_, err := svc.AddTags(params)

	if err != nil {
		fmt.Println(err.Error())
		return err
	}

	return nil

}

func plans_handler(params martini.Params, r render.Render) {
	r.JSON(200, plans)
}

func status_handler(param martini.Params, r render.Render) {
	domainname := param["domainname"]
	spec, err := status(domainname)
	if err != nil {
		fmt.Println(err)
		var er ErrorResponse
		er.Error = err.Error()
                r.Header().Add("x-ignore-errors","true")
		r.JSON(503, er)
		return
	}
        _, err = json.Marshal(spec)
	if err != nil {
		fmt.Println(err)
		var er ErrorResponse
		er.Error = err.Error()
                r.Header().Add("x-ignore-errors","true")
		r.JSON(503, er)
		return
	}
	r.JSON(200, spec)
}

func provision_handler(pspec provisionspec, berr binding.Errors, r render.Render) {
	if berr != nil {
		fmt.Println(berr)
		var er ErrorResponse
		er.Error = berr[0].Message
		r.JSON(500, er)
		return
	}
	ok := validate_plan(pspec.Plan)
	if !ok {
		var er ErrorResponse
		er.Error = "Invalid plan"
		r.JSON(500, er)
		return
	}
	domainname, err := generate_name()
	if err != nil {
		fmt.Println(err)
		var er ErrorResponse
		er.Error = err.Error()
		r.JSON(500, er)
		return
	}
	err = provision(domainname, pspec)
	if err != nil {
		fmt.Println(err)
		var er ErrorResponse
		er.Error = err.Error()
		r.JSON(500, er)
		return
	}
	r.JSON(201, map[string]string{"message": "creation requested", "spec": "es:" + domainname})

}

func provision(domainname string, spec provisionspec) (e error) {

	esversion := os.Getenv("ES_VERSION")
	var volumesize int64
	var volumetype string
	var instancetype string
        var masterinstancetype string
        var mastercount int64
        var instancecount int64
        var kmspossible bool
	if spec.Plan == "micro" {
		volumesize = int64(10)
		volumetype = "gp2"
		instancetype = "t2.small.elasticsearch"
                kmspossible = false
	}

	if spec.Plan == "small" {
		volumesize = int64(20)
		volumetype = "gp2"
		instancetype = "t2.medium.elasticsearch"
                kmspossible = false
	}

	if spec.Plan == "medium" {
		volumesize = int64(40)
		volumetype = "gp2"
		instancetype = "m4.large.elasticsearch"
                kmspossible = true
	}

	if spec.Plan == "large" {
		volumesize = int64(80)
		volumetype = "gp2"
		instancetype = "m4.xlarge.elasticsearch"
                kmspossible = true
	}
        if spec.Plan == "premium-0" {
                volumesize = int64(100)
                volumetype = "gp2"
                instancetype = "m4.large.elasticsearch"
                masterinstancetype = "m4.large.elasticsearch"
                mastercount=3
                instancecount=4
        }

	accountnumber := os.Getenv("ACCOUNTNUMBER")

	snidcsv := os.Getenv("SUBNET_ID")
        snidarray := strings.Split(snidcsv,",")
        var snids []*string
        for _, element := range snidarray {
           newelement := fmt.Sprintf("%v",element)
           snids = append(snids, &newelement)
        }

	sgid := os.Getenv("SECURITY_GROUP_ID")
	sgids := []*string{&sgid}

        
    if !strings.Contains(spec.Plan,"premium") && !kmspossible {
	params := &elasticsearchservice.CreateElasticsearchDomainInput{
		DomainName:           aws.String(domainname),
		ElasticsearchVersion: aws.String(esversion),
		AccessPolicies:       aws.String("{\"Version\":\"2012-10-17\",\"Statement\":[{\"Effect\":\"Allow\",\"Principal\":{\"AWS\":\"*\"},\"Action\":\"es:*\",\"Resource\":\"arn:aws:es:" + region + ":" + accountnumber + ":domain/" + domainname + "/*\"}]}"),
		EBSOptions: &elasticsearchservice.EBSOptions{
			EBSEnabled: aws.Bool(true),
			VolumeSize: aws.Int64(volumesize),
			VolumeType: aws.String(volumetype),
		},
		ElasticsearchClusterConfig: &elasticsearchservice.ElasticsearchClusterConfig{
			DedicatedMasterEnabled: aws.Bool(false),
			InstanceCount:          aws.Int64(1),
			InstanceType:           aws.String(instancetype),
			ZoneAwarenessEnabled:   aws.Bool(false),
		},
		VPCOptions: &elasticsearchservice.VPCOptions{
			SecurityGroupIds: sgids,
			SubnetIds:        snids[1:],
		},
	}
	resp, err := svc.CreateElasticsearchDomain(params)

	if err != nil {
		fmt.Println(err.Error())
		return err
	}

	fmt.Println(resp)
      }
    if !strings.Contains(spec.Plan,"premium") && kmspossible {
        params := &elasticsearchservice.CreateElasticsearchDomainInput{
                DomainName:           aws.String(domainname),
                ElasticsearchVersion: aws.String(esversion),
                AccessPolicies:       aws.String("{\"Version\":\"2012-10-17\",\"Statement\":[{\"Effect\":\"Allow\",\"Principal\":{\"AWS\":\"*\"},\"Action\":\"es:*\",\"Resource\":\"arn:aws:es:" + region + ":" + accountnumber + ":domain/" + domainname + "/*\"}]}"),
                EBSOptions: &elasticsearchservice.EBSOptions{
                        EBSEnabled: aws.Bool(true),
                        VolumeSize: aws.Int64(volumesize),
                        VolumeType: aws.String(volumetype),
                },
                ElasticsearchClusterConfig: &elasticsearchservice.ElasticsearchClusterConfig{
                        DedicatedMasterEnabled: aws.Bool(false),
                        InstanceCount:          aws.Int64(1),
                        InstanceType:           aws.String(instancetype),
                        ZoneAwarenessEnabled:   aws.Bool(false),
                },
                EncryptionAtRestOptions: &elasticsearchservice.EncryptionAtRestOptions {
                   Enabled: aws.Bool(true),
                   KmsKeyId:   aws.String(os.Getenv("KMSKEYID")),
                },
                VPCOptions: &elasticsearchservice.VPCOptions{
                        SecurityGroupIds: sgids,
                        SubnetIds:        snids[1:],
                },
        }
        var err error
        _, err = svc.CreateElasticsearchDomain(params)

        if err != nil {
                fmt.Println(err.Error())
                return err
        }

      }

    if strings.Contains(spec.Plan,"premium") {
        params := &elasticsearchservice.CreateElasticsearchDomainInput{
                DomainName:           aws.String(domainname),
                ElasticsearchVersion: aws.String(esversion),
                AccessPolicies:       aws.String("{\"Version\":\"2012-10-17\",\"Statement\":[{\"Effect\":\"Allow\",\"Principal\":{\"AWS\":\"*\"},\"Action\":\"es:*\",\"Resource\":\"arn:aws:es:" + region + ":" + accountnumber + ":domain/" + domainname + "/*\"}]}"),
                EBSOptions: &elasticsearchservice.EBSOptions{
                        EBSEnabled: aws.Bool(true),
                        VolumeSize: aws.Int64(volumesize),
                        VolumeType: aws.String(volumetype),
                },
                ElasticsearchClusterConfig: &elasticsearchservice.ElasticsearchClusterConfig{
                        DedicatedMasterEnabled: aws.Bool(true),
                        DedicatedMasterCount:   aws.Int64(mastercount),
                        DedicatedMasterType:    aws.String(masterinstancetype),
                        InstanceCount:          aws.Int64(instancecount),
                        InstanceType:           aws.String(instancetype),
                        ZoneAwarenessEnabled:   aws.Bool(true),
                },
                EncryptionAtRestOptions: &elasticsearchservice.EncryptionAtRestOptions {
                   Enabled: aws.Bool(true),
                   KmsKeyId:   aws.String(os.Getenv("KMSKEYID")),
                },
                VPCOptions: &elasticsearchservice.VPCOptions{
                        SubnetIds:        snids,
                        SecurityGroupIds: sgids,
                },
        }
        var err error
        _, err = svc.CreateElasticsearchDomain(params)

        if err != nil {
                fmt.Println(err.Error())
                return err
        }

     }

	var billingcode tagspec
	billingcode.Resource = domainname
	billingcode.Name = "billingcode"
	billingcode.Value = spec.Billingcode
	err := tag(billingcode)
	if err != nil {
		fmt.Println(err.Error())
		return err
	}
	err = addtodb(domainname, spec)
	if err != nil {
		fmt.Println(err)
		return err
	}
	return nil
}

func status(domainname string) (s ESSpec, e error) {
	var spec ESSpec

	params := &elasticsearchservice.DescribeElasticsearchDomainInput{
		DomainName: aws.String(domainname), // Required
	}
	resp, err := svc.DescribeElasticsearchDomain(params)

	if err != nil {
		return spec, err
	}

	if resp.DomainStatus.Endpoints != nil {
		spec.ES_URL = "https://" + *resp.DomainStatus.Endpoints["vpc"]
		spec.KIBANA_URL = "https://" + *resp.DomainStatus.Endpoints["vpc"] + "/_plugin/kibana"
	} else {
		return spec, errors.New("not available")
	}
	return spec, nil
}

func generate_name() (n string, e error) {
	var toreturn string
	domainname, err := uuid.NewV4()
	if err != nil {
		fmt.Println(err)
		return toreturn, err
	}
	toreturn = os.Getenv("NAME_PREFIX") + strings.Split(domainname.String(), "-")[0]
	return toreturn, nil

}

func validate_plan(plan string) (r bool) {
	ok := plans[plan]
	if ok != "" {
		return true
	} else {
		return false
	}
}

func createdb() (e error) {

	sqlStatement := `CREATE TABLE if not exists public.provision (  name character varying(200) NOT NULL,  plan character varying(200),  claimed character varying(200),  make_date timestamp without time zone DEFAULT now(),  CONSTRAINT name_pkey PRIMARY KEY (name) )`

	_, err := brokerdb_pool.Exec(sqlStatement)
	if err != nil {
		fmt.Println("unable to set up db")
		return err

	}
	return nil

}

func addtodb(domainname string, spec provisionspec) (e error) {

	_, err := brokerdb_pool.Exec("INSERT INTO provision(name,plan,claimed) VALUES($1,$2,$3) returning name;", domainname, spec.Plan, "yes")
	if err != nil {
		fmt.Println(err)
		return err
	}
	return nil

}

func deletefromdb(domainname string) (e error) {
	stmt, err := brokerdb_pool.Prepare("delete from provision where name=$1")
	if err != nil {
		fmt.Println(err)
		return err
	}
	res, err := stmt.Exec(domainname)
	if err != nil {
		fmt.Println(err)
		return err
	}
	_, err = res.RowsAffected()
	if err != nil {
		fmt.Println(err)
		return
	}
	return nil
}

func getDB(uri string) (d *sql.DB, e error) {
	db, dberr := sql.Open("postgres", uri)
	if dberr != nil {
		fmt.Println(dberr)
		return nil, dberr
	}
	db.SetMaxIdleConns(5)
	db.SetMaxOpenConns(20)
	return db, nil
}
